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

func startSQLite(t *testing.T) (dsn string, terminate func()) {
	t.Helper()

	// Use a shared in-memory SQLite database for tests.

	// The shared cache ensures multiple connections see the same in-memory DB.
	return "file:memdb1?mode=memory&cache=shared", func() {}
}

func TestPGStore_ResumePoint(t *testing.T) {
	dsn, stop := startSQLite(t)
	defer stop()

	ctx := context.Background()
	db, err := idxstore.NewIndexStore(dsn, ctx)
	if err != nil {
		t.Fatalf("NewIndexStore: %v", err)
	}

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
	dsn, stop := startSQLite(t)
	defer stop()

	ctx := context.Background()
	db, err := idxstore.NewIndexStore(dsn, ctx)
	if err != nil {
		t.Fatalf("NewIndexStore: %v", err)
	}

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
	dsn, stop := startSQLite(t)
	defer stop()

	ctx := context.Background()
	db, err := idxstore.NewIndexStore(dsn, ctx)
	if err != nil {
		t.Fatalf("NewIndexStore: %v", err)
	}

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

func bytesOf(b byte, n int) []byte {
	out := make([]byte, n)

	for i := 0; i < n; i++ {
		out[i] = b
	}

	return out
}
