package index

import (
	"bytes"
	"encoding/hex"
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

const trimIntervalBlocks = 100 // Trim UTXOs every N blocks

var Zeroes = [32]byte{}

type Indexer struct {
	governor.ServiceCtx
	_db            spec.Store
	db             spec.Store
	blocks         chan walker.BlockOrUndo
	trimSpentAfter int64
}

/*
 * NewIndexer creates an Indexer service that tracks the ChainState.
 *
 * `onlyScriptType` is an optional ScriptType to index (if this is 0,
 * all standard spendable UTXOs are indexed, including multisig.
 */
func NewIndexer(db spec.Store, blocks chan walker.BlockOrUndo, trimSpentAfter int64) governor.Service {
	return &Indexer{_db: db, blocks: blocks, trimSpentAfter: trimSpentAfter}
}

func (i *Indexer) Run() {
	i.db = i._db.WithCtx(i.Context) // bind to service context
	trimCounter := int64(0)
	for !i.Stopping() {
		cmd := <-i.blocks
		resumeHash, err := hex.DecodeString(cmd.LastProcessedBlock)
		if err != nil {
			log.Printf("[Indexer] cannot decode 'ResumeFromBlock' hex (from DogeWalker): %v", err)
			i.Sleep(RETRY_DELAY)
		}
		if cmd.Block != nil {
			// next block.
			//log.Printf("[%v] %v", cmd.Height, cmd.Block.Hash)
			var removeUTXOs []spec.OutPointKey
			var createUTXOs []spec.UTXO
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
					// Only index spendable outputs.
					if out.Value >= DUST_LIMIT {
						typ, compact := doge.ClassifyScript(out.Script)
						if typ != doge.ScriptTypeNonStandard && typ != doge.ScriptTypeNullData {
							createUTXOs = append(createUTXOs, spec.UTXO{
								TxID:   txID,
								VOut:   uint32(vout),
								Value:  out.Value,
								Type:   byte(typ),
								Script: compact,
							})
						}
					}
				}
			}
			if removeUTXOs != nil || createUTXOs != nil {
				// We cannot admit failure here (we would de-sync from ChainState),
				// so keep trying until someone fixes the DB, or someone stops
				// the Indexer and fixes a bug.
				for !i.Stopping() {
					err := i.db.Transact(func(tx spec.StoreTx) error {
						if removeUTXOs != nil {
							err := tx.RemoveUTXOs(removeUTXOs, cmd.Height)
							if err != nil {
								return err
							}
						}
						if createUTXOs != nil {
							err := tx.CreateUTXOs(createUTXOs, cmd.Height)
							if err != nil {
								return err
							}
						}
						return tx.SetResumePoint(resumeHash)
					})
					if err == nil {
						break
					}
					log.Printf("[Indexer] commit failed (will retry): %v", err)
					i.Sleep(RETRY_DELAY)
				}
			}
			log.Printf("[%v] %v DONE", cmd.Height, cmd.Block.Hash)
		} else if cmd.Undo != nil {
			log.Printf("[%v] undo to: %v", cmd.Undo.LastValidHeight, cmd.Undo.LastValidHash)
			// undo blocks.
			// We cannot admit failure here (we would de-sync from ChainState),
			// so keep trying until someone fixes the DB, or someone stops
			// the Indexer and fixes a bug.
			for !i.Stopping() {
				err := i.db.Transact(func(tx spec.StoreTx) error {
					err := tx.UndoAbove(cmd.Height)
					if err != nil {
						return err
					}
					return tx.SetResumePoint(resumeHash)
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
		trimCounter += 1
		if trimCounter >= trimIntervalBlocks {
			trimCounter = 0
			// Trim spent UTXOs older than 'trimSpentAfter' blocks
			trimHeight := cmd.Height - i.trimSpentAfter
			if trimHeight > 1 {
				log.Printf("[Indexer] trim older than: %v", trimHeight)
				err := i.db.TrimSpentUTXOs(trimHeight)
				if err != nil {
					log.Printf("[Indexer] trim failed: %v", err)
				}
			}
		}
	}
}
