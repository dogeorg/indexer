package index

import (
	"bytes"
	"encoding/hex"
	"log"
	"sync"
	"time"

	"github.com/dogeorg/doge"
	"github.com/dogeorg/dogewalker/walker"
	"github.com/dogeorg/governor"
	"github.com/dogeorg/indexer/spec"
)

const RETRY_DELAY = 5 * time.Second // for RPC and Database errors.
const ONE_DOGE = 100_000_000        // 1 DOGE
const DUST_LIMIT = ONE_DOGE / 100   // 0.01 DOGE

const trimIntervalBlocks = 1000 // Trim UTXOs every N blocks
const maxBlockHistory = 10      // Keep last 10 blocks in memory

var Zeroes = [32]byte{}

// BlockHistory represents a processed block for monitoring
type BlockHistory struct {
	Height         int64         `json:"height"`
	Hash           string        `json:"hash"`
	Timestamp      time.Time     `json:"timestamp"`
	TxCount        int           `json:"tx_count"`
	UTXOCreated    int           `json:"utxo_created"`
	UTXOSpent      int           `json:"utxo_spent"`
	ProcessingTime time.Duration `json:"processing_time_ms"`
}

// IndexerMonitor interface for accessing indexer state
type IndexerMonitor interface {
	GetBlockHistory() []BlockHistory
}

type Indexer struct {
	governor.ServiceCtx
	_db            spec.Store
	db             spec.Store
	blocks         chan walker.BlockOrUndo
	trimSpentAfter int64

	// In-memory block history for monitoring
	blockHistory []BlockHistory
	historyMutex sync.RWMutex
}

// Ensure Indexer implements governor.Service
var _ governor.Service = (*Indexer)(nil)

// Ensure Indexer implements IndexerMonitor
var _ IndexerMonitor = (*Indexer)(nil)

/*
 * NewIndexer creates an Indexer service that tracks the ChainState.
 *
 * `onlyScriptType` is an optional ScriptType to index (if this is 0,
 * all standard spendable UTXOs are indexed, including multisig.
 */
func NewIndexer(db spec.Store, blocks chan walker.BlockOrUndo, trimSpentAfter int64) *Indexer {
	return &Indexer{_db: db, blocks: blocks, trimSpentAfter: trimSpentAfter}
}

// Run is the entry point for the Indexer service (called by Governor)
func (i *Indexer) Run() {
	i.db = i._db.WithCtx(i.Context) // bind to service context
	trimCounter := int64(0)
	done := i.Context.Done()
	for !i.Stopping() {
		var cmd walker.BlockOrUndo
		select {
		case cmd = <-i.blocks:
		case <-done:
			return // shutdown
		}
		resumeHash, err := hex.DecodeString(cmd.LastProcessedBlock)
		if err != nil {
			log.Printf("[Indexer] cannot decode 'ResumeFromBlock' hex (from DogeWalker): %v", err)
			i.Sleep(RETRY_DELAY)
		}
		if cmd.Block != nil {
			// next block.
			startTime := time.Now()
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
					if out.Value > 0 {
						typ, compact := doge.ClassifyScript(out.Script)
						if typ != doge.ScriptTypeNonStandard && typ != doge.ScriptTypeNullData {
							createUTXOs = append(createUTXOs, spec.UTXO{
								TxID:   txID,
								VOut:   uint32(vout),
								Value:  out.Value,
								Type:   typ,
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
						return tx.SetResumePoint(resumeHash, cmd.Height)
					})
					if err == nil {
						break
					}
					log.Printf("[Indexer] commit failed (will retry): %v", err)
					i.Sleep(RETRY_DELAY)
				}
			}

			// Record block in history
			processingTime := time.Since(startTime)
			i.recordBlockHistory(cmd.Height, cmd.Block.Hash, len(cmd.Block.Block.Tx), len(createUTXOs), len(removeUTXOs), processingTime)

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
					return tx.SetResumePoint(resumeHash, cmd.Height)
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

// GetBlockHistory returns a copy of the recent block history for monitoring
func (i *Indexer) GetBlockHistory() []BlockHistory {
	i.historyMutex.RLock()
	defer i.historyMutex.RUnlock()

	// Return a copy to avoid race conditions
	history := make([]BlockHistory, len(i.blockHistory))
	copy(history, i.blockHistory)
	return history
}

// recordBlockHistory adds a block to the sliding window history
func (i *Indexer) recordBlockHistory(height int64, hash string, txCount, utxoCreated, utxoSpent int, processingTime time.Duration) {
	i.historyMutex.Lock()
	defer i.historyMutex.Unlock()

	block := BlockHistory{
		Height:         height,
		Hash:           hash,
		Timestamp:      time.Now(),
		TxCount:        txCount,
		UTXOCreated:    utxoCreated,
		UTXOSpent:      utxoSpent,
		ProcessingTime: processingTime,
	}

	// Add to the beginning (most recent first)
	i.blockHistory = append([]BlockHistory{block}, i.blockHistory...)

	// Keep only the last maxBlockHistory blocks
	if len(i.blockHistory) > maxBlockHistory {
		i.blockHistory = i.blockHistory[:maxBlockHistory]
	}
}
