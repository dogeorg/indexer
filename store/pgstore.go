package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dogeorg/indexer/spec"
	"github.com/mattn/go-sqlite3"
)

type PGStore struct {
	_db *sql.DB
	tx  Queryable
	ctx context.Context
}

var _ spec.Store = &PGStore{}

// The common read-only parts of sql.DB and sql.Tx interfaces
type Queryable interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// WITHOUT ROWID: SQLite version 3.8.2 (2013-12-06) or later

const SQL_SCHEMA string = `
CREATE TABLE IF NOT EXISTS migration (
	version INTEGER NOT NULL DEFAULT 1
);
`

var MIGRATIONS = []struct {
	ver   int
	query string
}{}

// NewPGStore returns a spec.Store implementation that uses Postgres or SQLite
func NewPGStore(fileName string, ctx context.Context) (spec.Store, error) {
	backend := "sqlite3"
	if strings.HasPrefix(fileName, "postgres://") {
		// "postgres://user:password@host/dbname"
		backend = "postgres"
	}
	db, err := sql.Open(backend, fileName)
	store := &PGStore{
		_db: db,
		tx:  db, // initial `tx` is the database itself.
		ctx: ctx,
	}
	if err != nil {
		return store, dbErr(err, "opening database")
	}
	if backend == "sqlite3" {
		// limit concurrent access until we figure out a way to start transactions
		// with the BEGIN CONCURRENT statement in Go. Avoids "database locked" errors.
		db.SetMaxOpenConns(1)
	}
	err = store.initSchema()
	return store, err
}

func (s *PGStore) Close() {
	s._db.Close()
}

func (s *PGStore) initSchema() error {
	return s.doTxn("init schema", func(tx *sql.Tx) error {
		// apply migrations
		verRow := tx.QueryRow("SELECT version FROM migration LIMIT 1")
		var version int
		err := verRow.Scan(&version)
		if err != nil {
			// first-time database init (idempotent)
			_, err := tx.Exec(SQL_SCHEMA)
			if err != nil {
				return dbErr(err, "creating database schema")
			}
			// set up version table (idempotent)
			err = tx.QueryRow("SELECT version FROM migration LIMIT 1").Scan(&version)
			if err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					version = 1
					_, err = tx.Exec("INSERT INTO migration (version) VALUES (?)", version)
					if err != nil {
						return dbErr(err, "updating version")
					}
				} else {
					return dbErr(err, "querying version")
				}
			}
			// set up application data.
			err = s.initAppData(tx)
			if err != nil {
				return err
			}
		}
		initVer := version
		for _, m := range MIGRATIONS {
			if version < m.ver {
				_, err = tx.Exec(m.query)
				if err != nil {
					return dbErr(err, fmt.Sprintf("applying migration %v", m.ver))
				}
				version = m.ver
			}
		}
		if version != initVer {
			_, err = tx.Exec("UPDATE migration SET version=?", version)
			if err != nil {
				return dbErr(err, "updating version")
			}
		}
		return nil
	})
}

// initConfig creates any initial data rows in the database (application-specific)
func (s *PGStore) initAppData(tx *sql.Tx) error {
	return nil
}

// WithCtx returns the same Store interface, bound to a specific cancellable Context.
func (s *PGStore) WithCtx(ctx context.Context) spec.Store {
	return &PGStore{
		_db: s._db,
		tx:  s.tx,
		ctx: ctx,
	}
}

func (s *PGStore) Transact(fn func(tx spec.StoreTx) error) error {
	return s.doTxn("Transact", func(tx *sql.Tx) error {
		store := &PGStore{
			_db: s._db,
			tx:  tx, // replace 'tx' with an actual transaction.
			ctx: s.ctx,
		}
		return fn(store)
	})
}

func IsConflict(err error) bool {
	if sqErr, isSq := err.(sqlite3.Error); isSq {
		if sqErr.Code == sqlite3.ErrBusy || sqErr.Code == sqlite3.ErrLocked {
			return true
		}
	}
	return false
}

func (s PGStore) doTxn(name string, work func(tx *sql.Tx) error) error {
	limit := 120
	for {
		tx, err := s._db.Begin()
		if err != nil {
			return dbErr(err, "cannot begin transaction: "+name)
		}
		defer tx.Rollback()
		err = work(tx)
		if err != nil {
			if IsConflict(err) {
				limit--
				if limit != 0 {
					s.Sleep(250 * time.Millisecond)
					continue // try again
				}
			}
			// roll back and return error
			return err
		}
		err = tx.Commit()
		if err != nil {
			if IsConflict(err) {
				limit--
				if limit != 0 {
					s.Sleep(250 * time.Millisecond)
					continue // try again
				}
			}
			// roll back and return error
			return dbErr(err, "cannot commit: "+name)
		}
		// success
		return nil
	}
}

func (s PGStore) Sleep(dur time.Duration) {
	select {
	case <-s.ctx.Done():
	case <-time.After(dur):
	}
}

func dbErr(err error, where string) error {
	// MUST pass through ErrNotFound to fulfil the API contract!
	if errors.Is(err, spec.ErrNotFound) {
		return err
	}
	if sqErr, isSq := err.(sqlite3.Error); isSq {
		if sqErr.Code == sqlite3.ErrConstraint {
			// MUST detect 'AlreadyExists' to fulfil the API contract!
			// Constraint violation, e.g. a duplicate key.
			return spec.ErrAlreadyExists
		}
		if sqErr.Code == sqlite3.ErrBusy || sqErr.Code == sqlite3.ErrLocked {
			// SQLite has a single-writer policy, even in WAL (write-ahead) mode.
			// SQLite will return BUSY if the database is locked by another connection.
			// We treat this as a transient database conflict, and the caller should retry.
			return fmt.Errorf("PGStore: db-conflict: %w", err) // 'wraps' the error
		}
	}
	return fmt.Errorf("PGStore: %w", err) // 'wraps' the error
}

// STORE INTERFACE METHODS

func (s *PGStore) GetChainPos() string {
	return ""
}

// RemoveUTXOs marks UTXOs as spent at `height`
func (s *PGStore) RemoveUTXOs(removeUTXOs []spec.OutPointKey, height int64) {

}

// CreateUTXOs inserts new UTXOs at `height` (can replace Removed UTXOs)
func (s *PGStore) CreateUTXOs(createUTXOs []spec.UTXO, height int64) {

}

// UndoAbove removes created UTXOs and re-activates Removed UTXOs above `height`.
func (s *PGStore) UndoAbove(height int64) {

}

// TrimRemoved permanently deletes all 'Removed' UTXOs below `height`
func (s *PGStore) TrimRemoved(height int64) {

}
