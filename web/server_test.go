package web

import (
	"testing"

	"github.com/dogeorg/doge"
)

func TestUtxoKindFromVersionByte(t *testing.T) {
	tests := []struct {
		name        string
		versionByte byte
		expected    doge.ScriptType
	}{
		// P2PKH tests
		{
			name:        "DogeMainNet P2PKH",
			versionByte: doge.DogeMainNetChain.P2PKH_Address_Prefix,
			expected:    doge.ScriptTypeP2PKH,
		},
		{
			name:        "DogeTestNet P2PKH",
			versionByte: doge.DogeTestNetChain.P2PKH_Address_Prefix,
			expected:    doge.ScriptTypeP2PKH,
		},
		{
			name:        "DogeRegTest P2PKH",
			versionByte: doge.DogeRegTestChain.P2PKH_Address_Prefix,
			expected:    doge.ScriptTypeP2PKH,
		},
		{
			name:        "BitcoinMain P2PKH",
			versionByte: doge.BitcoinMainChain.P2PKH_Address_Prefix,
			expected:    doge.ScriptTypeP2PKH,
		},
		{
			name:        "BitcoinTest P2PKH",
			versionByte: doge.BitcoinTestChain.P2PKH_Address_Prefix,
			expected:    doge.ScriptTypeP2PKH,
		},
		// P2SH tests
		{
			name:        "DogeMainNet P2SH",
			versionByte: doge.DogeMainNetChain.P2SH_Address_Prefix,
			expected:    doge.ScriptTypeP2SH,
		},
		{
			name:        "DogeTestNet P2SH",
			versionByte: doge.DogeTestNetChain.P2SH_Address_Prefix,
			expected:    doge.ScriptTypeP2SH,
		},
		{
			name:        "DogeRegTest P2SH",
			versionByte: doge.DogeRegTestChain.P2SH_Address_Prefix,
			expected:    doge.ScriptTypeP2SH,
		},
		{
			name:        "BitcoinMain P2SH",
			versionByte: doge.BitcoinMainChain.P2SH_Address_Prefix,
			expected:    doge.ScriptTypeP2SH,
		},
		{
			name:        "BitcoinTest P2SH",
			versionByte: doge.BitcoinTestChain.P2SH_Address_Prefix,
			expected:    doge.ScriptTypeP2SH,
		},
		// P2PK tests
		{
			name:        "DogeMainNet P2PK",
			versionByte: doge.DogeMainNetChain.PKey_Prefix,
			expected:    doge.ScriptTypeP2PK,
		},
		{
			name:        "DogeTestNet P2PK",
			versionByte: doge.DogeTestNetChain.PKey_Prefix,
			expected:    doge.ScriptTypeP2PK,
		},
		{
			name:        "DogeRegTest P2PK",
			versionByte: doge.DogeRegTestChain.PKey_Prefix,
			expected:    doge.ScriptTypeP2PK,
		},
		{
			name:        "BitcoinMain P2PK",
			versionByte: doge.BitcoinMainChain.PKey_Prefix,
			expected:    doge.ScriptTypeP2PK,
		},
		{
			name:        "BitcoinTest P2PK",
			versionByte: doge.BitcoinTestChain.PKey_Prefix,
			expected:    doge.ScriptTypeP2PK,
		},
		// Invalid/unrecognized bytes
		{
			name:        "Invalid byte",
			versionByte: 0xFF,
			expected:    doge.ScriptTypeNone,
		},
		{
			name:        "Zero byte (Bitcoin P2PKH)",
			versionByte: 0x00,
			expected:    doge.ScriptTypeP2PKH, // 0x00 matches Bitcoin P2PKH prefix
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := utxoKindFromVersionByte(tt.versionByte)
			if result != tt.expected {
				t.Errorf("utxoKindFromVersionByte(%#x) = %v, expected %v", tt.versionByte, result, tt.expected)
			}
		})
	}
}
