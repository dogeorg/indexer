package spec_test

import (
	"encoding/json"
	"testing"

	"github.com/dogeorg/indexer/spec"
)

const (
	overInt64Koinu = "9223372036854775808"
	overInt64Doge  = "92233720368.54775808"
)

func TestBigKoinuJSONUsesExistingDogeStringFormat(t *testing.T) {
	amount := scanBigKoinu(t, overInt64Koinu)
	data, err := json.Marshal(amount)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(data) != `"`+overInt64Doge+`"` {
		t.Fatalf("MarshalJSON = %s", data)
	}
}

func TestBigKoinuScansNumericString(t *testing.T) {
	var amount spec.BigKoinu
	if err := amount.Scan(overInt64Koinu); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got := amount.String(); got != overInt64Doge {
		t.Fatalf("String() = %q, want %q", got, overInt64Doge)
	}
}

func TestBigKoinuRejectsInvalidDatabaseStrings(t *testing.T) {
	for _, input := range []string{"1.1", "abc"} {
		t.Run(input, func(t *testing.T) {
			var amount spec.BigKoinu
			if err := amount.Scan(input); err == nil {
				t.Fatalf("Scan(%q) succeeded, want error", input)
			}
		})
	}
}

func TestBigKoinuAddsCurrentBalanceBuckets(t *testing.T) {
	current := scanBigKoinu(t, "100").Add(scanBigKoinu(t, "50"))
	if current.String() != scanBigKoinu(t, "150").String() {
		t.Fatalf("current = %s, want %s", current, scanBigKoinu(t, "150"))
	}
}

func scanBigKoinu(t *testing.T, value string) spec.BigKoinu {
	t.Helper()
	var amount spec.BigKoinu
	if err := amount.Scan(value); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return amount
}
