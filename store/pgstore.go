package store

import (
	"context"

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

const SCHEMA_v0 = `
`

var MIGRATIONS = []storelib.Migration{
	{Version: 0, SQL: SCHEMA_v0},
}

// STORE INTERFACE

func (s *IndexStore) GetChainPos() string {
	return ""
}

// RemoveUTXOs marks UTXOs as spent at `height`
func (s *IndexStore) RemoveUTXOs(removeUTXOs []spec.OutPointKey, height int64) {

}

// CreateUTXOs inserts new UTXOs at `height` (can replace Removed UTXOs)
func (s *IndexStore) CreateUTXOs(createUTXOs []spec.UTXO, height int64) {

}

// UndoAbove removes created UTXOs and re-activates Removed UTXOs above `height`.
func (s *IndexStore) UndoAbove(height int64) {

}

// TrimRemoved permanently deletes all 'Removed' UTXOs below `height`
func (s *IndexStore) TrimRemoved(height int64) {

}
