package spec

import (
	"github.com/dogeorg/storelib"
)

type StoreTx interface {

	// GetResumePoint gets the hash to resume from.
	GetResumePoint() (hash []byte, err error)

	// SetResumePoint sets the hash to resume from.
	SetResumePoint(hash []byte) error

	// RemoveUTXOs marks UTXOs as spent at `height`
	RemoveUTXOs(removeUTXOs []OutPointKey, height int64) error

	// CreateUTXOs inserts new UTXOs at `height`
	CreateUTXOs(createUTXOs []UTXO, height int64) error

	// FindUTXOs finds all unspent UTXOs for an address.
	FindUTXOs(kind byte, address []byte) (res []UTXO, err error)

	// UndoAbove removes created UTXOs and re-activates Removed UTXOs above `height`.
	UndoAbove(height int64) error

	// TrimSpentUTXOs permanently deletes all spent UTXOs below `height`
	TrimSpentUTXOs(height int64) error
}

type Store interface {
	storelib.StoreAPI[Store, StoreTx] // include the Base Store API
	StoreTx                           // include all the StoreTx methods
}
