package spec

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/dogeorg/doge/koinu"
)

var (
	koinuPerDoge = big.NewInt(int64(koinu.OneDoge))
)

// BigKoinu is a koinu amount stored in a wider integer type than koinu.Koinu.
//
// Individual UTXOs fit in koinu.Koinu, but address-level sums can exceed int64.
type BigKoinu struct {
	value big.Int
}

var _ json.Marshaler = BigKoinu{}

func (a BigKoinu) Add(other BigKoinu) BigKoinu {
	var res BigKoinu
	res.value.Add(&a.value, &other.value)
	return res
}

func (a BigKoinu) Equal(other BigKoinu) bool {
	return a.value.Cmp(&other.value) == 0
}

func (a BigKoinu) String() string {
	if a.value.Sign() == 0 {
		return "0"
	}

	var abs big.Int
	abs.Abs(&a.value)

	var whole big.Int
	var part big.Int
	whole.QuoRem(&abs, koinuPerDoge, &part)

	sign := ""
	if a.value.Sign() < 0 {
		sign = "-"
	}
	if part.Sign() == 0 {
		return sign + whole.String()
	}

	decimal := fmt.Sprintf("%08s", part.String())
	decimal = strings.TrimRight(decimal, "0")
	return fmt.Sprintf("%s%s.%s", sign, whole.String(), decimal)
}

func (a BigKoinu) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf("%q", a.String())), nil
}

func (a *BigKoinu) Scan(value any) error {
	switch v := value.(type) {
	case int64:
		a.value.SetInt64(v)
		return nil
	case []byte:
		return a.setString(string(v))
	case string:
		return a.setString(v)
	default:
		return fmt.Errorf("cannot scan %T into BigKoinu", value)
	}
}

func (a *BigKoinu) setString(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		a.value.SetInt64(0)
		return nil
	}

	original := value
	sign := 1
	switch {
	case strings.HasPrefix(value, "-"):
		sign = -1
		value = value[1:]
	case strings.HasPrefix(value, "+"):
		value = value[1:]
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("invalid big koinu %q", original)
	}

	if before, after, found := strings.Cut(value, "."); found {
		if strings.Trim(after, "0") == "" {
			// PostgreSQL NUMERIC values may include a trailing .000...
			if before == "" || before == "0" {
				a.value.SetInt64(0)
				return nil
			}
			if _, ok := a.value.SetString(before, 10); !ok {
				return fmt.Errorf("invalid big koinu %q", original)
			}
			if sign < 0 {
				a.value.Neg(&a.value)
			}
			return nil
		}
		return a.setDogeString(before, after, sign, original)
	}

	if _, ok := a.value.SetString(value, 10); !ok {
		return fmt.Errorf("invalid big koinu %q", original)
	}
	if sign < 0 {
		a.value.Neg(&a.value)
	}
	return nil
}

func (a *BigKoinu) setDogeString(whole, frac string, sign int, original string) error {
	if len(frac) > 8 {
		return fmt.Errorf("big koinu has at most 8 decimal places, got %q", original)
	}
	for _, r := range whole {
		if r < '0' || r > '9' {
			return fmt.Errorf("invalid big koinu %q", original)
		}
	}
	for _, r := range frac {
		if r < '0' || r > '9' {
			return fmt.Errorf("invalid big koinu %q", original)
		}
	}

	if whole == "" {
		whole = "0"
	}

	var wholePart big.Int
	if _, ok := wholePart.SetString(whole, 10); !ok {
		return fmt.Errorf("invalid big koinu %q", original)
	}

	fracPadded := frac + strings.Repeat("0", 8-len(frac))
	var fracPart big.Int
	if _, ok := fracPart.SetString(fracPadded, 10); !ok {
		return fmt.Errorf("invalid big koinu %q", original)
	}

	var koinu big.Int
	koinu.Mul(&wholePart, koinuPerDoge)
	koinu.Add(&koinu, &fracPart)
	if sign < 0 {
		koinu.Neg(&koinu)
	}
	a.value.Set(&koinu)
	return nil
}
