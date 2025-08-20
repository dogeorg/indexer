package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/dogeorg/doge"
	"github.com/dogeorg/indexer/spec"
	"github.com/dogeorg/storelib"
)

// Type aliases to enhance readability
type Store = spec.Store
type StoreTx = spec.StoreTx
type StoreBase = storelib.StoreBase[Store, StoreTx]
type StoreImpl = storelib.StoreImpl[Store, StoreTx]

type IndexStore struct {
	StoreBase
}

var _ Store = &IndexStore{} // interface assertion

// NewIndexStore returns a spec.Store implementation that uses Postgres or SQLite
func NewIndexStore(fileName string, ctx context.Context) (Store, error) {
	store := &IndexStore{}
	err := storelib.InitStore(store, &store.StoreBase, fileName, MIGRATIONS, ctx)
	return store, err
}

// Clone makes a copy of the store implementation (because storelib can't do this part)
func (s *IndexStore) Clone() (StoreImpl, *StoreBase, Store, StoreTx) {
	newstore := &IndexStore{}
	return newstore, &newstore.StoreBase, newstore, newstore
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

var MIGRATIONS = []storelib.Migration{
	{Version: 1, SQL: SCHEMA_v0},
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
	return nil
}

// RemoveUTXOs marks UTXOs as spent at `height`
func (s *IndexStore) RemoveUTXOs(removeUTXOs []spec.OutPointKey, height int64) error {
	query, err := s.Txn.Prepare(`UPDATE utxo SET spent=$1 WHERE vout=$2 AND txid=(SELECT txid FROM tx WHERE hash=$3)`)
	if err != nil {
		return err
	}
	for _, out := range removeUTXOs {
		_, err := query.Exec(height, out.VOut, out.Tx)
		if err != nil {
			return s.DBErr(err, "RemoveUTXOs")
		}
	}
	return nil
}

// CreateUTXOs inserts new UTXOs at `height` (can replace Removed UTXOs)
func (s *IndexStore) CreateUTXOs(createUTXOs []spec.UTXO, height int64) error {
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
	row := s.Txn.QueryRow(`SELECT
		(SELECT COALESCE(SUM(u.value),0) FROM utxo u INNER JOIN tx t ON u.txid = t.txid WHERE u.script=$1 AND u.kind=$2 AND t.height < (SELECT height FROM resume LIMIT 1)-$3 AND u.spent IS NULL),
		(SELECT COALESCE(SUM(u.value),0) FROM utxo u INNER JOIN tx t ON u.txid = t.txid WHERE u.script=$1 AND u.kind=$2 AND t.height >= (SELECT height FROM resume LIMIT 1)-$3 AND u.spent IS NULL),
		(SELECT COALESCE(SUM(u.value),0) FROM utxo u INNER JOIN tx t ON u.txid = t.txid WHERE u.script=$1 AND u.kind=$2 AND u.spent >= (SELECT height FROM resume LIMIT 1)-$3)`,
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
	_, err = s.Txn.Exec(`DELETE FROM tx WHERE txid IN (SELECT t.txid FROM tx t LEFT OUTER JOIN utxo u ON t.txid = u.txid AND u.txid IS NULL)`)
	if err != nil {
		return s.DBErr(err, "TrimRemoved")
	}
	return nil
}
