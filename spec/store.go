package spec

import "context"

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
	StoreTx

	// Transact performs a transactional update
	Transact(fn func(tx StoreTx) error) error

	// WithCtx returns the same Store interface, bound to a specific cancellable Context
	WithCtx(ctx context.Context) Store

	// Close closes the database.
	Close()
}
