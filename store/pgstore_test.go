package store_test

import (
	"context"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/dogeorg/doge"
	"github.com/dogeorg/doge/koinu"
	"github.com/dogeorg/indexer/spec"
	idxstore "github.com/dogeorg/indexer/store"
)

func newTestStore(t *testing.T) (spec.Store, func()) {
	t.Helper()
	ctx := context.Background()

	// Use a unique in-memory database for each test to ensure isolation
	// Using ":memory:" creates a temporary database that's isolated per connection
	db, err := idxstore.NewIndexStore(":memory:", ctx)
	if err != nil {
		t.Fatalf("NewIndexStore: %v", err)
	}

	return db, func() {}
}

func TestPGStore_ResumePoint(t *testing.T) {
	db, stop := newTestStore(t)
	defer stop()

	// Initially empty
	got, err := db.GetResumePoint()
	if err != nil {
		t.Fatalf("GetResumePoint (initial): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no resume point, got: %x", got)
	}

	// Set first resume point
	hash1 := bytesOf(0x11, 32)
	if err := db.Transact(func(tx spec.StoreTx) error {
		return tx.SetResumePoint(hash1, 100)
	}); err != nil {
		t.Fatalf("SetResumePoint(1): %v", err)
	}

	got, err = db.GetResumePoint()
	if err != nil {
		t.Fatalf("GetResumePoint (after set 1): %v", err)
	}
	if string(got) != string(hash1) {
		t.Fatalf("resume point mismatch: got %x, want %x", got, hash1)
	}

	// Update resume point
	hash2 := bytesOf(0x22, 32)
	if err := db.Transact(func(tx spec.StoreTx) error {
		return tx.SetResumePoint(hash2, 200)
	}); err != nil {
		t.Fatalf("SetResumePoint(2): %v", err)
	}
	got, err = db.GetResumePoint()
	if err != nil {
		t.Fatalf("GetResumePoint (after set 2): %v", err)
	}
	if string(got) != string(hash2) {
		t.Fatalf("resume point mismatch after update: got %x, want %x", got, hash2)
	}
}

func TestPGStore_UTXO_Create_Remove_Find_Balance(t *testing.T) {
	db, stop := newTestStore(t)
	defer stop()

	// Prepare sample data
	kind := doge.ScriptTypeP2PKH
	address := bytesOf(0xAA, 20)

	txA := bytesOf(0xA1, 32)
	txB := bytesOf(0xB2, 32)

	utxoA := spec.UTXO{
		TxID:   txA,
		VOut:   0,
		Value:  1000,
		Type:   kind,
		Script: address, // compact form for P2PKH is the 20-byte hash
	}
	utxoB := spec.UTXO{
		TxID:   txB,
		VOut:   1,
		Value:  2000,
		Type:   kind,
		Script: address,
	}

	// Create UTXOs at height 100 and set head (resume) to 101 so they are "Available" with 0 conf
	if err := db.Transact(func(tx spec.StoreTx) error {
		if err := tx.CreateUTXOs([]spec.UTXO{utxoA, utxoB}, 100); err != nil {
			return err
		}
		return tx.SetResumePoint(bytesOf(0xD1, 32), 101)
	}); err != nil {
		t.Fatalf("CreateUTXOs/SetResumePoint: %v", err)
	}

	// Find
	found, err := db.FindUTXOs(kind, address)
	if err != nil {
		t.Fatalf("FindUTXOs: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("FindUTXOs count = %d, want 2", len(found))
	}

	// Balance with 0 conf at head=101: both UTXOs are available (height 100 < 101)
	bal, err := db.GetBalance(kind, address, 0)
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if bal.Available != koinu.Koinu(3000) || bal.Incoming != 0 || bal.Outgoing != 0 {
		t.Fatalf("Balance after create = {A:%d I:%d O:%d}, want {A:3000 I:0 O:0}", bal.Available, bal.Incoming, bal.Outgoing)
	}

	// Spend utxoA at height 106, set head to 106 so it's counted as Outgoing with 0 conf
	if err := db.Transact(func(tx spec.StoreTx) error {
		if err := tx.RemoveUTXOs([]spec.OutPointKey{spec.OutPoint(utxoA.TxID, utxoA.VOut)}, 106); err != nil {
			return err
		}
		return tx.SetResumePoint(bytesOf(0xD2, 32), 106)
	}); err != nil {
		t.Fatalf("RemoveUTXOs/SetResumePoint: %v", err)
	}

	// Find should now return only utxoB
	found, err = db.FindUTXOs(kind, address)
	if err != nil {
		t.Fatalf("FindUTXOs (after remove): %v", err)
	}
	if len(found) != 1 || string(found[0].TxID) != string(txB) || found[0].VOut != 1 {
		t.Fatalf("FindUTXOs result unexpected: %+v", found)
	}

	// Balance at head=106, 0 conf:
	// - Available: utxoB (2000)
	// - Outgoing: utxoA (1000) because spent(106) >= head(106) - 0
	bal, err = db.GetBalance(kind, address, 0)
	if err != nil {
		t.Fatalf("GetBalance (after remove): %v", err)
	}
	if bal.Available != koinu.Koinu(2000) || bal.Incoming != 0 || bal.Outgoing != koinu.Koinu(1000) {
		t.Fatalf("Balance after remove = {A:%d I:%d O:%d}, want {A:2000 I:0 O:1000}", bal.Available, bal.Incoming, bal.Outgoing)
	}
}

func TestPGStore_UndoAbove(t *testing.T) {
	db, stop := newTestStore(t)
	defer stop()

	kind := doge.ScriptTypeP2PKH
	addr := bytesOf(0xCC, 20)

	txA := bytesOf(0xA3, 32)
	txB := bytesOf(0xB4, 32)
	txC := bytesOf(0xC5, 32)

	utxoA := spec.UTXO{TxID: txA, VOut: 0, Value: 1000, Type: kind, Script: addr}
	utxoB := spec.UTXO{TxID: txB, VOut: 1, Value: 2000, Type: kind, Script: addr}
	utxoC := spec.UTXO{TxID: txC, VOut: 0, Value: 3000, Type: kind, Script: addr}

	// Create A and B at height 100 and set resume
	if err := db.Transact(func(tx spec.StoreTx) error {
		if err := tx.CreateUTXOs([]spec.UTXO{utxoA, utxoB}, 100); err != nil {
			return err
		}
		return tx.SetResumePoint(bytesOf(0xEE, 32), 101)
	}); err != nil {

		t.Fatalf("CreateUTXOs/SetResumePoint: %v", err)
	}

	// Spend A at 105 and B at 107
	if err := db.Transact(func(tx spec.StoreTx) error {
		if err := tx.RemoveUTXOs([]spec.OutPointKey{spec.OutPoint(utxoA.TxID, utxoA.VOut)}, 105); err != nil {
			return err
		}
		if err := tx.RemoveUTXOs([]spec.OutPointKey{spec.OutPoint(utxoB.TxID, utxoB.VOut)}, 107); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatalf("RemoveUTXOs: %v", err)
	}

	// Create C at height 110
	if err := db.Transact(func(tx spec.StoreTx) error {
		return tx.CreateUTXOs([]spec.UTXO{utxoC}, 110)
	}); err != nil {
		t.Fatalf("CreateUTXOs(C): %v", err)
	}

	// UndoAbove(106): should unspend B (spent 107) but keep A spent (105) and delete C (110)
	if err := db.Transact(func(tx spec.StoreTx) error {
		return tx.UndoAbove(106)
	}); err != nil {
		t.Fatalf("UndoAbove: %v", err)
	}

	// After undo, Find should return only B
	found, err := db.FindUTXOs(kind, addr)
	if err != nil {
		t.Fatalf("FindUTXOs: %v", err)
	}

	if len(found) != 1 || string(found[0].TxID) != string(txB) || found[0].VOut != utxoB.VOut {
		t.Fatalf("FindUTXOs after UndoAbove unexpected: %+v", found)
	}
}

func TestPGStore_GetCurrentHeight(t *testing.T) {
	db, stop := newTestStore(t)
	defer stop()

	// Get initial height (should be 0 for a fresh database)
	initialHeight, err := db.GetCurrentHeight()
	if err != nil {
		t.Fatalf("GetCurrentHeight (initial): %v", err)
	}
	if initialHeight != 0 {
		t.Fatalf("expected initial height 0, got: %d", initialHeight)
	}

	// Set height
	hash1 := bytesOf(0xAA, 32)
	if err := db.Transact(func(tx spec.StoreTx) error {
		return tx.SetResumePoint(hash1, 100)
	}); err != nil {
		t.Fatalf("SetResumePoint: %v", err)
	}

	height, err := db.GetCurrentHeight()
	if err != nil {
		t.Fatalf("GetCurrentHeight (after set): %v", err)
	}
	if height != 100 {
		t.Fatalf("expected height 100, got: %d", height)
	}

	// Update height
	hash2 := bytesOf(0xBB, 32)
	if err := db.Transact(func(tx spec.StoreTx) error {
		return tx.SetResumePoint(hash2, 200)
	}); err != nil {
		t.Fatalf("SetResumePoint (update): %v", err)
	}

	height, err = db.GetCurrentHeight()
	if err != nil {
		t.Fatalf("GetCurrentHeight (after update): %v", err)
	}
	if height != 200 {
		t.Fatalf("expected height 200, got: %d", height)
	}
}

func TestPGStore_TrimSpentUTXOs(t *testing.T) {
	db, stop := newTestStore(t)
	defer stop()

	kind := doge.ScriptTypeP2PKH
	addr := bytesOf(0x11, 20)

	txA := bytesOf(0xA1, 32)
	txB := bytesOf(0xB2, 32)
	txC := bytesOf(0xC3, 32)

	utxoA := spec.UTXO{TxID: txA, VOut: 0, Value: 1000, Type: kind, Script: addr}
	utxoB := spec.UTXO{TxID: txB, VOut: 0, Value: 2000, Type: kind, Script: addr}
	utxoC := spec.UTXO{TxID: txC, VOut: 0, Value: 3000, Type: kind, Script: addr}

	// Create A, B, C at different heights
	if err := db.Transact(func(tx spec.StoreTx) error {
		if err := tx.CreateUTXOs([]spec.UTXO{utxoA}, 100); err != nil {
			return err
		}
		if err := tx.CreateUTXOs([]spec.UTXO{utxoB}, 101); err != nil {
			return err
		}
		if err := tx.CreateUTXOs([]spec.UTXO{utxoC}, 102); err != nil {
			return err
		}
		return tx.SetResumePoint(bytesOf(0xDD, 32), 103)
	}); err != nil {
		t.Fatalf("CreateUTXOs/SetResumePoint: %v", err)
	}

	// Spend A at height 104
	if err := db.Transact(func(tx spec.StoreTx) error {
		if err := tx.RemoveUTXOs([]spec.OutPointKey{spec.OutPoint(utxoA.TxID, utxoA.VOut)}, 104); err != nil {
			return err
		}
		return tx.SetResumePoint(bytesOf(0xEE, 32), 104)
	}); err != nil {
		t.Fatalf("RemoveUTXOs/SetResumePoint: %v", err)
	}

	// Spend B at height 105
	if err := db.Transact(func(tx spec.StoreTx) error {
		if err := tx.RemoveUTXOs([]spec.OutPointKey{spec.OutPoint(utxoB.TxID, utxoB.VOut)}, 105); err != nil {
			return err
		}
		return tx.SetResumePoint(bytesOf(0xFF, 32), 105)
	}); err != nil {
		t.Fatalf("RemoveUTXOs/SetResumePoint: %v", err)
	}

	// Verify we can still find utxoA and utxoB (spent but not trimmed)
	found, err := db.FindUTXOs(kind, addr)
	if err != nil {
		t.Fatalf("FindUTXOs: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("FindUTXOs count = %d, want 1 (only utxoC unspent)", len(found))
	}
	if string(found[0].TxID) != string(txC) {
		t.Fatalf("FindUTXOs should only return utxoC")
	}

	// Trim spent UTXOs below height 105 (should remove A and B)
	if err := db.Transact(func(tx spec.StoreTx) error {
		return tx.TrimSpentUTXOs(105)
	}); err != nil {
		t.Fatalf("TrimSpentUTXOs: %v", err)
	}

	// After trim, Find should still return only utxoC
	found, err = db.FindUTXOs(kind, addr)
	if err != nil {
		t.Fatalf("FindUTXOs (after trim): %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("FindUTXOs count = %d, want 1 after trim", len(found))
	}

	// Spend C at height 106
	if err := db.Transact(func(tx spec.StoreTx) error {
		if err := tx.RemoveUTXOs([]spec.OutPointKey{spec.OutPoint(utxoC.TxID, utxoC.VOut)}, 106); err != nil {
			return err
		}
		return tx.SetResumePoint(bytesOf(0xAA, 32), 106)
	}); err != nil {
		t.Fatalf("RemoveUTXOs/SetResumePoint: %v", err)
	}

	// Trim C at height 107 (should remove C but keep nothing else)
	if err := db.Transact(func(tx spec.StoreTx) error {
		return tx.TrimSpentUTXOs(107)
	}); err != nil {
		t.Fatalf("TrimSpentUTXOs: %v", err)
	}

	// Should find nothing now
	found, err = db.FindUTXOs(kind, addr)
	if err != nil {
		t.Fatalf("FindUTXOs (after final trim): %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("FindUTXOs should return empty after all are trimmed, got %d", len(found))
	}
}

func TestPGStore_Balance_Confirmations(t *testing.T) {
	db, stop := newTestStore(t)
	defer stop()

	kind := doge.ScriptTypeP2PKH
	addr := bytesOf(0x22, 20)

	txA := bytesOf(0xA1, 32)
	txB := bytesOf(0xB2, 32)
	txC := bytesOf(0xC3, 32)

	utxoA := spec.UTXO{TxID: txA, VOut: 0, Value: 1000, Type: kind, Script: addr}
	utxoB := spec.UTXO{TxID: txB, VOut: 0, Value: 2000, Type: kind, Script: addr}
	utxoC := spec.UTXO{TxID: txC, VOut: 0, Value: 3000, Type: kind, Script: addr}

	// Create A at height 100
	if err := db.Transact(func(tx spec.StoreTx) error {
		if err := tx.CreateUTXOs([]spec.UTXO{utxoA}, 100); err != nil {
			return err
		}
		return tx.SetResumePoint(bytesOf(0x11, 32), 105) // head = 105
	}); err != nil {
		t.Fatalf("CreateUTXOs/SetResumePoint: %v", err)
	}

	// At head=105, utxoA at height 100: with 6 confirmations, height 100 < 105-6 = 99 is FALSE
	// So it goes to Incoming
	bal, err := db.GetBalance(kind, addr, 6)
	if err != nil {
		t.Fatalf("GetBalance (6 conf): %v", err)
	}
	if bal.Available != 0 {
		t.Fatalf("Balance with 6 conf: Available = %d, want 0", bal.Available)
	}
	if bal.Incoming != koinu.Koinu(1000) {
		t.Fatalf("Balance with 6 conf: Incoming = %d, want 1000", bal.Incoming)
	}

	// With 0 confirmations, available (height 100 < 105-0 = 105)
	bal, err = db.GetBalance(kind, addr, 0)
	if err != nil {
		t.Fatalf("GetBalance (0 conf): %v", err)
	}
	if bal.Available != koinu.Koinu(1000) {
		t.Fatalf("Balance with 0 conf = %d Available, want 1000", bal.Available)
	}

	// Move head to 107
	if err := db.Transact(func(tx spec.StoreTx) error {
		return tx.SetResumePoint(bytesOf(0x22, 32), 107)
	}); err != nil {
		t.Fatalf("SetResumePoint: %v", err)
	}

	// Now with 6 confirmations: height 100 < 107-6 = 101, so available
	bal, err = db.GetBalance(kind, addr, 6)
	if err != nil {
		t.Fatalf("GetBalance (6 conf, head 107): %v", err)
	}
	if bal.Available != koinu.Koinu(1000) {
		t.Fatalf("Balance with 6 conf at head 107 = %d Available, want 1000", bal.Available)
	}

	// Create B and C at heights that will be "Incoming" with 6 conf
	if err := db.Transact(func(tx spec.StoreTx) error {
		// B at height 106 (7 blocks behind head)
		if err := tx.CreateUTXOs([]spec.UTXO{utxoB}, 106); err != nil {
			return err
		}
		// C at height 107 (same as head)
		if err := tx.CreateUTXOs([]spec.UTXO{utxoC}, 107); err != nil {
			return err
		}
		return tx.SetResumePoint(bytesOf(0x33, 32), 110) // move head to 110
	}); err != nil {
		t.Fatalf("CreateUTXOs/SetResumePoint: %v", err)
	}

	// At head=110:
	// - A at 100: height 100 < (110-6=104), so Available (1000)
	// - B at 106: height 106 < (110-6=104) is FALSE, height 106 >= (110-6=104), so Incoming (2000)
	// - C at 107: height 107 >= (110-6=104), so Incoming (3000)
	bal, err = db.GetBalance(kind, addr, 6)
	if err != nil {
		t.Fatalf("GetBalance (6 conf, complex): %v", err)
	}
	if bal.Available != koinu.Koinu(1000) {
		t.Fatalf("Available = %d, want 1000", bal.Available)
	}
	if bal.Incoming != koinu.Koinu(5000) {
		t.Fatalf("Incoming = %d, want 5000", bal.Incoming)
	}

	// Spend A at height 111
	if err := db.Transact(func(tx spec.StoreTx) error {
		if err := tx.RemoveUTXOs([]spec.OutPointKey{spec.OutPoint(utxoA.TxID, utxoA.VOut)}, 111); err != nil {
			return err
		}
		return tx.SetResumePoint(bytesOf(0x44, 32), 111)
	}); err != nil {
		t.Fatalf("RemoveUTXOs/SetResumePoint: %v", err)
	}

	// At head=111, with 6 conf:
	// - A spent at 111: spent(111) >= (111-6=105), so Outgoing (1000)
	// - B at 106: Incoming (2000)
	// - C at 107: Incoming (3000)
	bal, err = db.GetBalance(kind, addr, 6)
	if err != nil {
		t.Fatalf("GetBalance (after spend): %v", err)
	}
	if bal.Available != 0 {
		t.Fatalf("Available = %d, want 0", bal.Available)
	}
	if bal.Incoming != koinu.Koinu(5000) {
		t.Fatalf("Incoming = %d, want 5000", bal.Incoming)
	}
	if bal.Outgoing != koinu.Koinu(1000) {
		t.Fatalf("Outgoing = %d, want 1000", bal.Outgoing)
	}
}

func TestPGStore_DifferentScriptTypes(t *testing.T) {
	db, stop := newTestStore(t)
	defer stop()

	// Test different script types - use unique addresses
	addrP2PKH := bytesOf(0x33, 20)
	addrP2SH := bytesOf(0x44, 20)
	addrP2PK := bytesOf(0x55, 20)
	addrP2PKHW := bytesOf(0x66, 20)

	txA := bytesOf(0x77, 32)
	txB := bytesOf(0x88, 32)
	txC := bytesOf(0x99, 32)
	txD := bytesOf(0xAA, 32)

	utxoP2PKH := spec.UTXO{TxID: txA, VOut: 0, Value: 1000, Type: doge.ScriptTypeP2PKH, Script: addrP2PKH}
	utxoP2SH := spec.UTXO{TxID: txB, VOut: 0, Value: 2000, Type: doge.ScriptTypeP2SH, Script: addrP2SH}
	utxoP2PK := spec.UTXO{TxID: txC, VOut: 0, Value: 3000, Type: doge.ScriptTypeP2PK, Script: addrP2PK}
	utxoP2PKHW := spec.UTXO{TxID: txD, VOut: 0, Value: 4000, Type: doge.ScriptTypeP2PKHW, Script: addrP2PKHW}

	if err := db.Transact(func(tx spec.StoreTx) error {
		if err := tx.CreateUTXOs([]spec.UTXO{utxoP2PKH, utxoP2SH, utxoP2PK, utxoP2PKHW}, 100); err != nil {
			return err
		}
		return tx.SetResumePoint(bytesOf(0xEE, 32), 101)
	}); err != nil {
		t.Fatalf("CreateUTXOs/SetResumePoint: %v", err)
	}

	// Find P2PKH UTXOs
	found, err := db.FindUTXOs(doge.ScriptTypeP2PKH, addrP2PKH)
	if err != nil {
		t.Fatalf("FindUTXOs (P2PKH): %v", err)
	}
	if len(found) != 1 || string(found[0].TxID) != string(txA) {
		t.Fatalf("FindUTXOs P2PKH failed: %+v", found)
	}

	// Find P2SH UTXOs
	found, err = db.FindUTXOs(doge.ScriptTypeP2SH, addrP2SH)
	if err != nil {
		t.Fatalf("FindUTXOs (P2SH): %v", err)
	}
	if len(found) != 1 || string(found[0].TxID) != string(txB) {
		t.Fatalf("FindUTXOs P2SH failed: %+v", found)
	}

	// Find P2PK UTXOs
	found, err = db.FindUTXOs(doge.ScriptTypeP2PK, addrP2PK)
	if err != nil {
		t.Fatalf("FindUTXOs (P2PK): %v", err)
	}
	if len(found) != 1 || string(found[0].TxID) != string(txC) {
		t.Fatalf("FindUTXOs P2PK failed: %+v", found)
	}

	// Find P2PKHW UTXOs
	found, err = db.FindUTXOs(doge.ScriptTypeP2PKHW, addrP2PKHW)
	if err != nil {
		t.Fatalf("FindUTXOs (P2PKHW): %v", err)
	}
	if len(found) != 1 || string(found[0].TxID) != string(txD) {
		t.Fatalf("FindUTXOs P2PKHW failed: %+v", found)
	}

	// Get balances for each
	bal, err := db.GetBalance(doge.ScriptTypeP2PKH, addrP2PKH, 0)
	if err != nil {
		t.Fatalf("GetBalance P2PKH: %v", err)
	}
	if bal.Available != koinu.Koinu(1000) {
		t.Fatalf("Balance P2PKH = %d, want 1000", bal.Available)
	}

	bal, err = db.GetBalance(doge.ScriptTypeP2SH, addrP2SH, 0)
	if err != nil {
		t.Fatalf("GetBalance P2SH: %v", err)
	}
	if bal.Available != koinu.Koinu(2000) {
		t.Fatalf("Balance P2SH = %d, want 2000", bal.Available)
	}

	bal, err = db.GetBalance(doge.ScriptTypeP2PK, addrP2PK, 0)
	if err != nil {
		t.Fatalf("GetBalance P2PK: %v", err)
	}
	if bal.Available != koinu.Koinu(3000) {
		t.Fatalf("Balance P2PK = %d, want 3000", bal.Available)
	}

	bal, err = db.GetBalance(doge.ScriptTypeP2PKHW, addrP2PKHW, 0)
	if err != nil {
		t.Fatalf("GetBalance P2PKHW: %v", err)
	}
	if bal.Available != koinu.Koinu(4000) {
		t.Fatalf("Balance P2PKHW = %d, want 4000", bal.Available)
	}
}

func bytesOf(b byte, n int) []byte {
	out := make([]byte, n)

	for i := 0; i < n; i++ {
		out[i] = b
	}

	return out
}
