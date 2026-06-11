package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/dogeorg/doge"
	"github.com/dogeorg/indexer/spec"
	"github.com/dogeorg/storelib"
)

// Type aliases to enhance readability
type Store = spec.Store
type StoreTx = spec.StoreTx
type StoreBase = storelib.StoreBase[Store, StoreTx]
type StoreImpl = storelib.StoreImpl[Store, StoreTx]

const defaultBalanceConfirmations = 6

type IndexStore struct {
	StoreBase
	cacheBalances bool
}

var _ Store = &IndexStore{} // interface assertion

// NewIndexStore returns a spec.Store implementation that uses Postgres or SQLite
func NewIndexStore(fileName string, ctx context.Context, cacheBalances bool) (Store, error) {
	store := &IndexStore{cacheBalances: cacheBalances}
	if store.cacheBalances && !isPostgresConnectionString(fileName) {
		return store, fmt.Errorf("cache balances requires a Postgres database")
	}
	err := storelib.InitStore(store, &store.StoreBase, fileName, MIGRATIONS, ctx)
	if err != nil {
		return store, err
	}
	if store.cacheBalances {
		err = store.withDBTxn(store.ensureBalancesReady)
	}
	return store, err
}

func (s *IndexStore) withDBTxn(fn func() error) error {
	tx, err := s.RawDB.Begin()
	if err != nil {
		return s.DBErr(err, "withDBTxn: begin")
	}
	defer tx.Rollback()

	prev := s.Txn
	s.Txn = tx
	defer func() { s.Txn = prev }()

	if err := fn(); err != nil {
		return err
	}
	return s.DBErr(tx.Commit(), "withDBTxn: commit")
}

// Clone makes a copy of the store implementation (because storelib can't do this part)
func (s *IndexStore) Clone() (StoreImpl, *StoreBase, Store, StoreTx) {
	newstore := &IndexStore{cacheBalances: s.cacheBalances}
	return newstore, &newstore.StoreBase, newstore, newstore
}

func isPostgresConnectionString(fileName string) bool {
	return strings.HasPrefix(fileName, "postgres://")
}

// DATABASE SCHEMA

// SMALLINT is int16, INTEGER is int32, BIGINT is int64
// HASH index uses a 4-byte hash of the indexed column, which is lossy/approx but compact
// kind IN (2,3,5,6) is (TX_PUBKEYHASH,TX_SCRIPTHASH,TX_WITNESS_V0_KEYHASH,TX_WITNESS_V0_SCRIPTHASH)
const SCHEMA_v0 = `
CREATE TABLE utxo (
	txid BIGINT NOT NULL,
	vout INTEGER NOT NULL,
	value BIGINT NOT NULL,
	kind SMALLINT NOT NULL,
	script BYTEA NOT NULL,
	spent BIGINT NULL,
	PRIMARY KEY (txid,vout)
);
CREATE INDEX address ON utxo USING HASH (script) WHERE kind IN (2,3,5,6);
CREATE TABLE tx (
	txid BIGSERIAL PRIMARY KEY,
	height BIGINT NOT NULL,
	hash BYTEA NOT NULL
);
CREATE INDEX tx_hash ON tx USING HASH (hash);
CREATE INDEX tx_height ON tx (height);
CREATE TABLE resume (
	hash BYTEA NOT NULL,
	height BIGINT NOT NULL
);
`

const SCHEMA_v1 = `
CREATE TABLE balance (
	kind SMALLINT NOT NULL,
	script BYTEA NOT NULL,
	available NUMERIC NOT NULL,
	incoming NUMERIC NOT NULL,
	outgoing NUMERIC NOT NULL,
	PRIMARY KEY (kind,script)
);
CREATE TABLE balance_meta (
	id SMALLINT PRIMARY KEY,
	height BIGINT NOT NULL,
	confirmations BIGINT NOT NULL
);
`

var MIGRATIONS = []storelib.Migration{
	{Version: 1, SQL: SCHEMA_v0},
	{Version: 2, SQL: SCHEMA_v1},
}

// STORE INTERFACE

func (s *IndexStore) GetResumePoint() ([]byte, error) {
	row := s.Txn.QueryRow(`SELECT hash FROM resume LIMIT 1`)
	var hash []byte
	err := row.Scan(&hash)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // empty array
		}
		return nil, s.DBErr(err, "GetResumePoint")
	}
	return hash, nil
}

func (s *IndexStore) SetResumePoint(hash []byte, height int64) error {
	res, err := s.Txn.Exec(`UPDATE resume SET hash=$1, height=$2`, hash, height)
	if err != nil {
		return s.DBErr(err, "SetResumePoint")
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return s.DBErr(err, "SetResumePoint RowsAffected")
	}
	if rows < 1 {
		// First time: insert the single row.
		_, err = s.Txn.Exec(`INSERT INTO resume (hash,height) VALUES ($1,$2)`, hash, height)
		if err != nil {
			return s.DBErr(err, "SetResumePoint Insert")
		}
	}
	if s.cacheBalances {
		if err := s.advanceBalances(height); err != nil {
			return err
		}
	}
	return nil
}

// GetCurrentHeight gets the current block height from the resume point.
func (s *IndexStore) GetCurrentHeight() (int64, error) {
	row := s.Txn.QueryRow(`SELECT height FROM resume LIMIT 1`)
	var height int64
	err := row.Scan(&height)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil // no blocks indexed yet
		}
		return 0, s.DBErr(err, "GetCurrentHeight")
	}
	return height, nil
}

func (s *IndexStore) ensureBalancesReady() error {
	height, err := s.GetCurrentHeight()
	if err != nil {
		return err
	}

	metaHeight, confirmations, found, err := s.getBalanceMeta()
	if err != nil {
		return err
	}
	if !found || metaHeight != height || confirmations != defaultBalanceConfirmations {
		return s.rebuildBalances(height)
	}
	return nil
}

func (s *IndexStore) getBalanceMeta() (height int64, confirmations int64, found bool, err error) {
	row := s.Txn.QueryRow(`SELECT height,confirmations FROM balance_meta WHERE id=1`)
	err = row.Scan(&height, &confirmations)
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, 0, false, nil
		}
		return 0, 0, false, s.DBErr(err, "getBalanceMeta")
	}
	return height, confirmations, true, nil
}

func (s *IndexStore) setBalanceMeta(height int64) error {
	_, err := s.Txn.Exec(`INSERT INTO balance_meta (id,height,confirmations) VALUES (1,$1,$2)
		ON CONFLICT (id) DO UPDATE SET
			height=excluded.height,
			confirmations=excluded.confirmations`, height, defaultBalanceConfirmations)
	if err != nil {
		return s.DBErr(err, "setBalanceMeta")
	}
	return nil
}

func (s *IndexStore) rebuildBalances(height int64) error {
	threshold := height - defaultBalanceConfirmations

	_, err := s.Txn.Exec(`DELETE FROM balance`)
	if err != nil {
		return s.DBErr(err, "rebuildBalances: clear balance")
	}
	_, err = s.Txn.Exec(`DELETE FROM balance_meta`)
	if err != nil {
		return s.DBErr(err, "rebuildBalances: clear balance_meta")
	}

	_, err = s.Txn.Exec(`INSERT INTO balance (kind,script,available,incoming,outgoing)
		SELECT
			u.kind,
			u.script,
			COALESCE(SUM(CASE WHEN u.spent IS NULL AND t.height < $1 THEN CAST(u.value AS NUMERIC) ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN u.spent IS NULL AND t.height >= $1 THEN CAST(u.value AS NUMERIC) ELSE 0 END),0),
			COALESCE(SUM(CASE WHEN u.spent >= $1 THEN CAST(u.value AS NUMERIC) ELSE 0 END),0)
		FROM utxo u
		INNER JOIN tx t ON u.txid = t.txid
		WHERE u.kind IN (2,3,5,6) AND (u.spent IS NULL OR u.spent >= $1)
		GROUP BY u.kind,u.script
		HAVING
			COALESCE(SUM(CASE WHEN u.spent IS NULL THEN CAST(u.value AS NUMERIC) ELSE 0 END),0) > 0 OR
			COALESCE(SUM(CASE WHEN u.spent >= $1 THEN CAST(u.value AS NUMERIC) ELSE 0 END),0) > 0`,
		threshold)
	if err != nil {
		return s.DBErr(err, "rebuildBalances: insert balance")
	}

	return s.setBalanceMeta(height)
}

func (s *IndexStore) applyBalanceDelta(kind doge.ScriptType, script []byte, availableDelta int64, incomingDelta int64, outgoingDelta int64) error {
	if availableDelta == 0 && incomingDelta == 0 && outgoingDelta == 0 {
		return nil
	}

	_, err := s.Txn.Exec(`INSERT INTO balance (kind,script,available,incoming,outgoing)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (kind,script) DO UPDATE SET
			available=available+excluded.available,
			incoming=incoming+excluded.incoming,
			outgoing=outgoing+excluded.outgoing`,
		kind, script, availableDelta, incomingDelta, outgoingDelta)
	if err != nil {
		return s.DBErr(err, "applyBalanceDelta: upsert")
	}

	_, err = s.Txn.Exec(`DELETE FROM balance WHERE kind=$1 AND script=$2 AND available=0 AND incoming=0 AND outgoing=0`, kind, script)
	if err != nil {
		return s.DBErr(err, "applyBalanceDelta: prune")
	}
	return nil
}

func (s *IndexStore) balanceCacheHeight() (int64, error) {
	height, _, found, err := s.getBalanceMeta()
	if err != nil {
		return 0, err
	}
	if !found {
		if err := s.ensureBalancesReady(); err != nil {
			return 0, err
		}
		height, _, found, err = s.getBalanceMeta()
		if err != nil {
			return 0, err
		}
	}
	if !found {
		return 0, fmt.Errorf("balanceCacheHeight: balance metadata missing")
	}
	return height, nil
}

func balanceIsAvailable(txHeight int64, currentHeight int64, confirmations int64) bool {
	return txHeight < currentHeight-confirmations
}

func spendIsOutgoing(spentHeight int64, currentHeight int64, confirmations int64) bool {
	return spentHeight >= currentHeight-confirmations
}

func cacheableBalanceKind(kind doge.ScriptType) bool {
	switch kind {
	case doge.ScriptTypeP2PKH, doge.ScriptTypeP2SH, doge.ScriptTypeP2PKHW, doge.ScriptTypeP2SHW:
		return true
	default:
		return false
	}
}

func (s *IndexStore) advanceBalances(height int64) error {
	oldHeight, confirmations, found, err := s.getBalanceMeta()
	if err != nil {
		return err
	}
	if !found {
		return s.rebuildBalances(height)
	}
	if confirmations != defaultBalanceConfirmations || height < oldHeight {
		return s.rebuildBalances(height)
	}
	if height == oldHeight {
		return s.setBalanceMeta(height)
	}

	oldThreshold := oldHeight - defaultBalanceConfirmations
	newThreshold := height - defaultBalanceConfirmations

	_, err = s.Txn.Exec(`INSERT INTO balance (kind,script,available,incoming,outgoing)
		SELECT u.kind,u.script,COALESCE(SUM(CAST(u.value AS NUMERIC)),0),-COALESCE(SUM(CAST(u.value AS NUMERIC)),0),0
		FROM utxo u
		INNER JOIN tx t ON u.txid = t.txid
		WHERE u.kind IN (2,3,5,6) AND u.spent IS NULL AND t.height >= $1 AND t.height < $2
		GROUP BY u.kind,u.script
		ON CONFLICT (kind,script) DO UPDATE SET
			available=balance.available+excluded.available,
			incoming=balance.incoming+excluded.incoming,
			outgoing=balance.outgoing+excluded.outgoing`, oldThreshold, newThreshold)
	if err != nil {
		return s.DBErr(err, "advanceBalances: mature")
	}

	_, err = s.Txn.Exec(`INSERT INTO balance (kind,script,available,incoming,outgoing)
		SELECT u.kind,u.script,0,0,-COALESCE(SUM(CAST(u.value AS NUMERIC)),0)
		FROM utxo u
		WHERE u.kind IN (2,3,5,6) AND u.spent >= $1 AND u.spent < $2
		GROUP BY u.kind,u.script
		ON CONFLICT (kind,script) DO UPDATE SET
			available=balance.available+excluded.available,
			incoming=balance.incoming+excluded.incoming,
			outgoing=balance.outgoing+excluded.outgoing`, oldThreshold, newThreshold)
	if err != nil {
		return s.DBErr(err, "advanceBalances: outgoing")
	}

	return s.setBalanceMeta(height)
}

// RemoveUTXOs marks UTXOs as spent at `height`
func (s *IndexStore) RemoveUTXOs(removeUTXOs []spec.OutPointKey, height int64) error {
	query, err := s.Txn.Prepare(`UPDATE utxo SET spent=$1 WHERE vout=$2 AND txid=(SELECT txid FROM tx WHERE hash=$3)`)
	if err != nil {
		return err
	}
	var currentHeight int64
	if s.cacheBalances {
		currentHeight, err = s.balanceCacheHeight()
		if err != nil {
			return err
		}
	}
	for _, out := range removeUTXOs {
		var kind doge.ScriptType
		var script []byte
		var value int64
		var txHeight int64
		found := false
		if s.cacheBalances {
			row := s.Txn.QueryRow(`SELECT u.kind,u.script,u.value,t.height
				FROM utxo u
				INNER JOIN tx t ON u.txid = t.txid
				WHERE u.kind IN (2,3,5,6) AND u.vout=$1 AND t.hash=$2 AND u.spent IS NULL`, out.VOut, out.Tx)
			err = row.Scan(&kind, &script, &value, &txHeight)
			if err != nil && err != sql.ErrNoRows {
				return s.DBErr(err, "RemoveUTXOs: lookup")
			}
			found = err == nil
		}

		_, err := query.Exec(height, out.VOut, out.Tx)
		if err != nil {
			return s.DBErr(err, "RemoveUTXOs")
		}
		if s.cacheBalances && found {
			availableDelta := int64(0)
			incomingDelta := int64(0)
			outgoingDelta := int64(0)
			if balanceIsAvailable(txHeight, currentHeight, defaultBalanceConfirmations) {
				availableDelta = -value
			} else {
				incomingDelta = -value
			}
			if spendIsOutgoing(height, currentHeight, defaultBalanceConfirmations) {
				outgoingDelta = value
			}
			if err := s.applyBalanceDelta(kind, script, availableDelta, incomingDelta, outgoingDelta); err != nil {
				return err
			}
		}
	}
	return nil
}

// CreateUTXOs inserts new UTXOs at `height` (can replace Removed UTXOs)
func (s *IndexStore) CreateUTXOs(createUTXOs []spec.UTXO, height int64) error {
	var currentHeight int64
	var err error
	if s.cacheBalances {
		currentHeight, err = s.balanceCacheHeight()
		if err != nil {
			return err
		}
	}

	// insert all required `tx` rows and cache the mapping to txid
	// no conflict expected: we delete tx on rollback, and hash is unique in Core
	txidMap := map[string]int64{} // hash -> txid
	txStmt, err := s.Txn.Prepare(`INSERT INTO tx (height,hash) VALUES ($1,$2) RETURNING txid`)
	if err != nil {
		return s.DBErr(err, "CreateUTXOs: prepare tx")
	}
	for _, utxo := range createUTXOs {
		hashKey := string(utxo.TxID) // binary string from hash bytes
		if _, found := txidMap[hashKey]; !found {
			row := txStmt.QueryRow(height, utxo.TxID)
			var txid int64
			err = row.Scan(&txid)
			if err != nil {
				return s.DBErr(err, "CreateUTXOs: insert tx")
			}
			txidMap[hashKey] = txid
		}
	}
	// insert all utxos
	utxoStmt, err := s.Txn.Prepare(`INSERT INTO utxo (txid,vout,value,kind,script) VALUES ($1,$2,$3,$4,$5)`)
	if err != nil {
		return err
	}
	for _, utxo := range createUTXOs {
		txid, found := txidMap[string(utxo.TxID)]
		if !found {
			return fmt.Errorf("CreateUTXOs: txid not found in map (BUG: was inserted above)")
		}
		// no conflict expected: we delete utxo on rollback, and (hash,vout) is unique in Core
		_, err := utxoStmt.Exec(txid, utxo.VOut, utxo.Value, utxo.Type, utxo.Script)
		if err != nil {
			return s.DBErr(err, "CreateUTXOs: insert utxo")
		}
		if s.cacheBalances && cacheableBalanceKind(utxo.Type) {
			availableDelta := int64(0)
			incomingDelta := utxo.Value
			if balanceIsAvailable(height, currentHeight, defaultBalanceConfirmations) {
				availableDelta = utxo.Value
				incomingDelta = 0
			}
			if err := s.applyBalanceDelta(utxo.Type, utxo.Script, availableDelta, incomingDelta, 0); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *IndexStore) FindUTXOs(kind doge.ScriptType, address []byte) (res []spec.UTXO, err error) {
	rows, err := s.Txn.Query(`SELECT t.hash,u.vout,u.value,u.script FROM utxo u INNER JOIN tx t ON u.txid = t.txid WHERE u.script=$1 AND u.kind=$2 AND u.spent IS NULL`, address, kind)
	if err != nil {
		return []spec.UTXO{}, s.DBErr(err, "FindUTXOs: query")
	}
	for rows.Next() {
		var hash []byte
		var vout uint32
		var value int64
		var script []byte
		err = rows.Scan(&hash, &vout, &value, &script)
		if err != nil {
			return []spec.UTXO{}, s.DBErr(err, "FindUTXOs: scan")
		}
		res = append(res, spec.UTXO{TxID: hash, VOut: vout, Value: value, Type: kind, Script: script})
	}
	if err = rows.Close(); err != nil {
		return []spec.UTXO{}, s.DBErr(err, "FindUTXOs: scan")
	}
	return res, nil
}

func (s *IndexStore) GetBalance(kind doge.ScriptType, address []byte, confirmations int64) (res spec.Balance, err error) {
	if s.cacheBalances && confirmations == defaultBalanceConfirmations && cacheableBalanceKind(kind) {
		row := s.Txn.QueryRow(`SELECT available,incoming,outgoing FROM balance WHERE script=$1 AND kind=$2`, address, kind)
		err = row.Scan(&res.Available, &res.Incoming, &res.Outgoing)
		if err != nil {
			if err == sql.ErrNoRows {
				return spec.Balance{}, nil
			}
			return spec.Balance{}, s.DBErr(err, "GetBalance: balance scan")
		}
		return res, nil
	}

	return s.getBalanceUncached(kind, address, confirmations)
}

func (s *IndexStore) getBalanceUncached(kind doge.ScriptType, address []byte, confirmations int64) (res spec.Balance, err error) {
	row := s.Txn.QueryRow(`SELECT
		(SELECT COALESCE(SUM(CAST(u.value AS NUMERIC)),0) FROM utxo u INNER JOIN tx t ON u.txid = t.txid WHERE u.script=$1 AND u.kind=$2 AND t.height < (SELECT height FROM resume LIMIT 1)-$3 AND u.spent IS NULL),
		(SELECT COALESCE(SUM(CAST(u.value AS NUMERIC)),0) FROM utxo u INNER JOIN tx t ON u.txid = t.txid WHERE u.script=$1 AND u.kind=$2 AND t.height >= (SELECT height FROM resume LIMIT 1)-$3 AND u.spent IS NULL),
		(SELECT COALESCE(SUM(CAST(u.value AS NUMERIC)),0) FROM utxo u INNER JOIN tx t ON u.txid = t.txid WHERE u.script=$1 AND u.kind=$2 AND u.spent >= (SELECT height FROM resume LIMIT 1)-$3)`,
		address, kind, confirmations)
	err = row.Scan(&res.Available, &res.Incoming, &res.Outgoing)
	if err != nil {
		return spec.Balance{}, s.DBErr(err, "GetBalance: scan")
	}
	return res, nil
}

// UndoAbove removes created UTXOs and re-activates Removed UTXOs above `height`.
func (s *IndexStore) UndoAbove(height int64) error {
	// undo inserting utxos.
	_, err := s.Txn.Exec(`DELETE FROM utxo WHERE txid IN (SELECT txid FROM tx WHERE height > $1)`, height)
	if err != nil {
		return s.DBErr(err, "UndoAbove: delete utxo")
	}
	// undo inserting txes.
	_, err = s.Txn.Exec(`DELETE FROM tx WHERE height > $1`, height)
	if err != nil {
		return s.DBErr(err, "UndoAbove: delete tx")
	}
	// undo marking utxos spent.
	_, err = s.Txn.Exec(`UPDATE utxo SET spent=NULL WHERE spent > $1`, height)
	if err != nil {
		return s.DBErr(err, "UndoAbove: unmark spent")
	}
	if s.cacheBalances {
		return s.rebuildBalances(height)
	}
	return nil
}

// TrimSpentUTXOs permanently deletes all 'Removed' UTXOs below `height`
func (s *IndexStore) TrimSpentUTXOs(height int64) error {
	// only considers utxos with 'spent' non-null
	_, err := s.Txn.Exec(`DELETE FROM utxo WHERE spent < $1`, height)
	if err != nil {
		return s.DBErr(err, "TrimRemoved")
	}
	// prune tx entries that no longer have any utxos
	// (delete all TX without a matching UTXO: after joining, UTXO-fields are NULL)
	_, err = s.Txn.Exec(`DELETE FROM tx WHERE txid IN (SELECT t.txid FROM tx t LEFT OUTER JOIN utxo u ON t.txid = u.txid WHERE u.txid IS NULL)`)
	if err != nil {
		return s.DBErr(err, "TrimRemoved")
	}
	return nil
}
