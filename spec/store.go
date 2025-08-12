package spec

import (
	"github.com/dogeorg/doge"
	"github.com/dogeorg/doge/koinu"
	"github.com/dogeorg/storelib"
)

type StoreTx interface {

	// GetResumePoint gets the hash to resume from.
	GetResumePoint() (hash []byte, err error)

	// SetResumePoint sets the hash to resume from.
	SetResumePoint(hash []byte, height int64) error

	// RemoveUTXOs marks UTXOs as spent at `height`
	RemoveUTXOs(removeUTXOs []OutPointKey, height int64) error

	// CreateUTXOs inserts new UTXOs at `height`
	CreateUTXOs(createUTXOs []UTXO, height int64) error

	// FindUTXOs finds all unspent UTXOs for an address.
	FindUTXOs(kind doge.ScriptType, address []byte) (res []UTXO, err error)

	// GetBalance sums all unspent UTXOs for an address.
	// 'confirmations' is the number of confirmations before a balance is available (typically 6)
	GetBalance(kind doge.ScriptType, address []byte, confirmations int64) (res Balance, err error)

	// UndoAbove removes created UTXOs and re-activates Removed UTXOs above `height`.
	UndoAbove(height int64) error

	// TrimSpentUTXOs permanently deletes all spent UTXOs below `height`
	TrimSpentUTXOs(height int64) error
}

type Store interface {
	storelib.StoreAPI[Store, StoreTx] // include the Base Store API
	StoreTx                           // include all the StoreTx methods
}

// Balance
type Balance struct {
	Incoming  koinu.Koinu `json:"incoming"`  // takes N confirmations to become Availble
	Available koinu.Koinu `json:"available"` // confirmed balance you can spend
	Outgoing  koinu.Koinu `json:"outgoing"`  // takes N confirmations to become fully Spent
	Current   koinu.Koinu `json:"current"`   // current balance: Incoming + Available
}
