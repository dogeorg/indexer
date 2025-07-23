package index

import (
	"bytes"
	"log"
	"time"

	"github.com/dogeorg/doge"
	"github.com/dogeorg/dogewalker/walker"
	"github.com/dogeorg/governor"
	"github.com/dogeorg/indexer/spec"
)

const RETRY_DELAY = 5 * time.Second // for RPC and Database errors.
const ONE_DOGE = 100_000_000        // 1 DOGE
const DUST_LIMIT = ONE_DOGE / 100   // 0.01 DOGE
const BATCH_SIZE = 10

var Zeroes = [32]byte{}

type Indexer struct {
	governor.ServiceCtx
	_db    spec.Store
	db     spec.Store
	blocks chan walker.BlockOrUndo
}

/*
 * NewIndexer creates an Indexer service that tracks the ChainState.
 *
 * `onlyScriptType` is an optional ScriptType to index (if this is 0,
 * all standard spendable UTXOs are indexed, including multisig.
 */
func NewIndexer(db spec.Store, blocks chan walker.BlockOrUndo) governor.Service {
	return &Indexer{_db: db, blocks: blocks}
}

func (i *Indexer) Run() {
	i.db = i._db.WithCtx(i.Context) // bind to service context
	for !i.Stopping() {
		cmd := <-i.blocks
		if cmd.Block != nil {
			// next block.
			log.Printf("[%v] %v", cmd.Height, cmd.ResumeFromBlock)
			removeUTXOs := []spec.OutPointKey{}
			createUTXOs := []spec.UTXO{}
			for _, tx := range cmd.Block.Block.Tx {
				txID := tx.TxID
				for _, in := range tx.VIn {
					// Ignore CoinBase input (all zeroes)
					if !bytes.Equal(in.TxID, Zeroes[:]) {
						removeUTXOs = append(removeUTXOs, spec.OutPoint(in.TxID, in.VOut))
					}
				}
				// Go does not support uint32 with range (vout is an int)
				// which theoretically could be a problem on a 32-bit system
				for vout, out := range tx.VOut {
					if out.Value >= DUST_LIMIT {
						typ, compact := doge.ClassifyScript(out.Script)
						if typ != doge.ScriptTypeNonStandard && typ != doge.ScriptTypeNullData {
							createUTXOs = append(createUTXOs, spec.UTXO{
								Key:    spec.OutPoint(txID, uint32(vout)),
								Value:  out.Value,
								Type:   byte(typ),
								Script: compact,
							})
						}
					}
				}
			}
			// We cannot admit failure here (we would de-sync from ChainState),
			// so keep trying until someone fixes the DB, or someone stops
			// the Indexer and fixes a bug.
			for !i.Stopping() {
				err := i.db.Transact(func(tx spec.StoreTx) error {
					tx.RemoveUTXOs(removeUTXOs, cmd.Height)
					tx.CreateUTXOs(createUTXOs, cmd.Height)
					return nil
				})
				if err == nil {
					break
				}
				log.Printf("[Indexer] commit failed (will retry): %v", err)
				i.Sleep(RETRY_DELAY)
			}
		} else if cmd.Undo != nil {
			log.Printf("[%v] undo to: %v", cmd.Height, cmd.ResumeFromBlock)
			// undo blocks.
			// We cannot admit failure here (we would de-sync from ChainState),
			// so keep trying until someone fixes the DB, or someone stops
			// the Indexer and fixes a bug.
			for !i.Stopping() {
				err := i.db.Transact(func(tx spec.StoreTx) error {
					tx.UndoAbove(cmd.Height)
					return nil
				})
				if err == nil {
					break
				}
				log.Printf("[Indexer] commit failed (will retry): %v", err)
				i.Sleep(RETRY_DELAY)
			}
		} else {
			// idle: nothing to do.
		}
	}
}
