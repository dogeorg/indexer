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

	if before, after, found := strings.Cut(value, "."); found {
		if strings.Trim(after, "0") != "" {
			return fmt.Errorf("big koinu must be an integer, got %q", value)
		}
		value = before
	}

	if _, ok := a.value.SetString(value, 10); !ok {
		return fmt.Errorf("invalid big koinu %q", value)
	}
	return nil
}
