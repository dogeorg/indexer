package spec

import (
	"github.com/dogeorg/storelib"
)

type StoreTx interface {

	// GetChainPos gets the last block hash we indexed.
	GetChainPos() string

	// RemoveUTXOs marks UTXOs as spent at `height`
	RemoveUTXOs(removeUTXOs []OutPointKey, height int64)

	// CreateUTXOs inserts new UTXOs at `height`
	CreateUTXOs(createUTXOs []UTXO, height int64)

	// UndoAbove removes created UTXOs and re-activates Removed UTXOs above `height`.
	UndoAbove(height int64)

	// TrimRemoved permanently deletes all 'Removed' UTXOs below `height`
	TrimRemoved(height int64)
}

type Store interface {
	storelib.StoreAPI[Store, StoreTx] // include the Base Store API
	StoreTx                           // include all the StoreTx methods
}
