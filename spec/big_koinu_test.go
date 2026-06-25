package spec_test

import (
	"encoding/json"
	"testing"

	"github.com/dogeorg/indexer/spec"
)

const (
	overInt64Koinu = "9223372036854775808" // one greater than int64 max (9223372036854775807)
	overInt64Doge  = "92233720368.54775808" // as above but in DOGE
)

// Input: 9223372036854775808 koinu
// JSON:  "92233720368.54775808" DOGE
func TestBigKoinuJSONUsesExistingDogeStringFormat(t *testing.T) {
	amount := scanBigKoinu(t, overInt64Koinu)

	data, err := json.Marshal(amount)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}

	want := `"` + overInt64Doge + `"`
	if got := string(data); got != want {
		t.Fatalf("MarshalJSON = %s, want %s", got, want)
	}
}

// Scan:   9223372036854775808 koinu (database integer)
// String: 92233720368.54775808 DOGE
func TestBigKoinuScansNumericString(t *testing.T) {
	var amount spec.BigKoinu
	if err := amount.Scan(overInt64Koinu); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got := amount.String(); got != overInt64Doge {
		t.Fatalf("String() = %q, want %q", got, overInt64Doge)
	}
}

// Scan:   92233720368.54775808 DOGE (API / String() format)
// String: 92233720368.54775808 DOGE
func TestBigKoinuScansDOGEDecimalString(t *testing.T) {
	var amount spec.BigKoinu
	if err := amount.Scan(overInt64Doge); err != nil {
		t.Fatalf("Scan(%q): %v", overInt64Doge, err)
	}
	if got := amount.String(); got != overInt64Doge {
		t.Fatalf("String() = %q, want %q", got, overInt64Doge)
	}
}

// koinu → DOGE: 9223372036854775808 → 92233720368.54775808
// DOGE → DOGE: 92233720368.54775808 → 92233720368.54775808
func TestBigKoinuRoundTripsDogeString(t *testing.T) {
	first := scanBigKoinu(t, overInt64Koinu)

	doge := first.String()
	if doge != overInt64Doge {
		t.Fatalf("String() = %q, want %q", doge, overInt64Doge)
	}

	var second spec.BigKoinu
	if err := second.Scan(doge); err != nil {
		t.Fatalf("Scan(%q): %v", doge, err)
	}
	if got := second.String(); got != overInt64Doge {
		t.Fatalf("String() = %q, want %q", got, overInt64Doge)
	}
}

// Scan:   9223372036854775808.0 (PostgreSQL NUMERIC trailing zeros)
// String: 92233720368.54775808 DOGE
func TestBigKoinuScansPGNumericTrailingZeros(t *testing.T) {
	var amount spec.BigKoinu
	if err := amount.Scan(overInt64Koinu + ".0"); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got := amount.String(); got != overInt64Doge {
		t.Fatalf("String() = %q, want %q", got, overInt64Doge)
	}
}

// Scan:   100 koinu (no decimal — database integer, not DOGE)
// String: 0.000001 DOGE (100 koinu, not 100 DOGE)
func TestBigKoinuScansIntegerKoinuWithoutDecimal(t *testing.T) {
	var amount spec.BigKoinu
	if err := amount.Scan("100"); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got := amount.String(); got != "0.000001" {
		t.Fatalf("String() = %q, want %q", got, "0.000001")
	}
}

// Scan:   1.1 DOGE (110000000 koinu)
// String: 1.1 DOGE
func TestBigKoinuScansDogeDecimalAmount(t *testing.T) {
	var amount spec.BigKoinu
	if err := amount.Scan("1.1"); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if got := amount.String(); got != "1.1" {
		t.Fatalf("String() = %q, want %q", got, "1.1")
	}
}

// Scan invalid strings → error:
//   - "abc"           not numeric
//   - "1.1.1"         two decimal points
//   - "1.123456789"   more than 8 decimal places
func TestBigKoinuRejectsInvalidDatabaseStrings(t *testing.T) {
	tests := []struct {
		input string
		why   string
	}{
		{input: "abc", why: "not numeric"},
		{input: "1.1.1", why: "two decimal points"},
		{input: "1.123456789", why: "more than 8 decimal places"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var amount spec.BigKoinu
			if err := amount.Scan(tt.input); err == nil {
				t.Fatalf("Scan(%q) succeeded, want error (%s)", tt.input, tt.why)
			}
		})
	}
}

// 100 koinu + 50 koinu = 150 koinu
// String: 0.00000150 DOGE
func TestBigKoinuAddsCurrentBalanceBuckets(t *testing.T) {
	current := scanBigKoinu(t, "100").Add(scanBigKoinu(t, "50"))
	want := scanBigKoinu(t, "150")
	if got := current.String(); got != want.String() {
		t.Fatalf("current = %s, want %s", got, want)
	}
}

func scanBigKoinu(t *testing.T, value string) spec.BigKoinu {
	t.Helper()
	var amount spec.BigKoinu
	if err := amount.Scan(value); err != nil {
		t.Fatalf("Scan(%q): %v", value, err)
	}
	return amount
}
