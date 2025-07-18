package index

import "github.com/dogeorg/doge"

// CompactScript is a compressed form of the script.
type CompactScript []byte

// ScriptType is inferred from the script by pattern-matching the code.
type ScriptType = byte

// ScriptMask is used to select script-types to compact and store.
type ScriptMask = int

const MAX_OP_RETURN_RELAY = 83 // from Core IsStandard in policy.cpp

// ScriptType constants (must fit in a single byte for compact storage)
const (
	ScriptNone        ScriptType = 0 // Reserved
	ScriptP2PK        ScriptType = 1 // TX_PUBKEY (in Core)
	ScriptP2PKH       ScriptType = 2 // TX_PUBKEYHASH
	ScriptP2SH        ScriptType = 3 // TX_SCRIPTHASH
	ScriptMultiSig    ScriptType = 4 // TX_MULTISIG
	ScriptP2PKHW      ScriptType = 5 // TX_WITNESS_V0_KEYHASH
	ScriptP2SHW       ScriptType = 6 // TX_WITNESS_V0_SCRIPTHASH
	ScriptNullData    ScriptType = 7 // TX_NULL_DATA
	ScriptNonStandard ScriptType = 8 // TX_NONSTANDARD
)

// ScriptMask constants (these are bit values for masking)
const (
	MaskP2PK        ScriptMask = 1                               // TX_PUBKEY (in Core)
	MaskP2PKH       ScriptMask = 2                               // TX_PUBKEYHASH
	MaskP2SH        ScriptMask = 4                               // TX_SCRIPTHASH
	MaskMultiSig    ScriptMask = 8                               // TX_MULTISIG
	MaskP2PKHW      ScriptMask = 16                              // TX_WITNESS_V0_KEYHASH
	MaskP2SHW       ScriptMask = 32                              // TX_WITNESS_V0_SCRIPTHASH
	MaskNullData    ScriptMask = 64                              // TX_NULL_DATA
	MaskNonStandard ScriptMask = 128                             // TX_NONSTANDARD
	MaskPayTo       ScriptMask = MaskP2PK | MaskP2PKH | MaskP2SH // "pay to" scripts
	MaskWitness     ScriptMask = MaskP2PKHW | MaskP2SHW          // segwit scripts
)

func ClassifyAndCompactScript(script []byte, mask ScriptMask) (ScriptType, CompactScript) {
	L := len(script)
	// OP_RETURN
	if L > 0 && script[0] == doge.OP_RETURN && L <= MAX_OP_RETURN_RELAY {
		// standard OP_RETURN: up to MAX_OP_RETURN_RELAY bytes
		if (mask & MaskNullData) != 0 {
			return ScriptNullData, script[1:] // remove OP_RETURN, keep the data
		}
		return ScriptNone, nil // ignore the script
	}
	// P2PKH: OP_DUP OP_HASH160 <pubKeyHash:20> OP_EQUALVERIFY OP_CHECKSIG (25)
	if L == 25 && script[0] == doge.OP_DUP && script[1] == doge.OP_HASH160 && script[2] == 20 &&
		script[23] == doge.OP_EQUALVERIFY && script[24] == doge.OP_CHECKSIG {
		if (mask & MaskP2PKH) != 0 {
			// 20 bytes PubKey Hash
			compact := make([]byte, 20)
			copy(compact[0:20], script[3:23]) // 20 bytes
			return ScriptP2PKH, compact
		}
		return ScriptNone, nil // ignore the script
	}
	// P2PK: <compressedPubKey:33> OP_CHECKSIG
	if L == 35 && script[0] == 33 && script[34] == doge.OP_CHECKSIG {
		if (mask & MaskP2PK) != 0 {
			// 33 bytes PubKey
			compact := make([]byte, 33)
			copy(compact[0:33], script[1:34]) // 33 bytes
			return ScriptP2PK, compact
		}
		return ScriptNone, nil // ignore the script
	}
	// P2PK: <uncompressedPubKey:65> OP_CHECKSIG
	if L == 67 && script[0] == 65 && script[66] == doge.OP_CHECKSIG {
		if (mask & MaskP2PK) != 0 {
			// 65 bytes PubKey
			compact := make([]byte, 65)
			copy(compact[0:65], script[1:66]) // 65 bytes
			return ScriptP2PK, compact
		}
		return ScriptNone, nil // ignore the script
	}
	// P2SH: OP_HASH160 0x14 <hash> OP_EQUAL
	if L == 23 && script[0] == doge.OP_HASH160 && script[1] == 20 && script[22] == doge.OP_EQUAL {
		if (mask & MaskP2SH) != 0 {
			// 20 bytes Script Hash
			compact := make([]byte, 20)
			copy(compact[0:20], script[2:22]) // 20 bytes
			return ScriptP2SH, compact
		}
		return ScriptNone, nil // ignore the script
	}
	// OP_m <pubkey*n> OP_n OP_CHECKMULTISIG (or a non-standard script)
	if (mask & (MaskMultiSig | MaskNonStandard)) != 0 {
		if L >= 3+34 && script[L-1] == doge.OP_CHECKMULTISIG && isOP123(script[L-2]) && isOP123(script[0]) {
			// if standard: N >= 1 && N <= 3 && M >= 1 && M <= N from Core IsStandard in policy.cpp
			N_Keys := decodeOP_N(script[L-2])
			M_Keys := decodeOP_N(script[0])
			if M_Keys <= N_Keys {
				endOfKeys := L - 2 // first byte after key data
				ofs := 1
				for ofs < endOfKeys && N_Keys > 0 {
					if script[ofs] == 65 && ofs+66 <= endOfKeys {
						ofs += 66 // uncompressed public key
					} else if script[ofs] == 33 && ofs+34 <= endOfKeys {
						ofs += 34 // compressed public key
					} else {
						ofs = 0
						break // non-standard multisig script
					}
					N_Keys -= 1
				}
				if ofs == endOfKeys && N_Keys == 0 { // used all data + all N keys found
					// standard multisig script
					if (mask & MaskMultiSig) != 0 {
						// all of the script, minus the last OP_CHECKMULTISIG opcode
						return ScriptMultiSig, script[0 : L-1]
					} else {
						return ScriptNone, nil // ignore the multisig script
					}
				}
			}
			// otherwise a non-standard script that "looks like" multisig: fall through
		}
	}
	// non-standard script
	if (mask & MaskNonStandard) != 0 {
		compact := make([]byte, L+1)
		compact[0] = ScriptNonStandard
		copy(compact[1:], script[:]) // L bytes
		return ScriptNonStandard, nil
	}
	return ScriptNone, nil // ignore the script
}

func isOP123(op byte) bool {
	return op >= doge.OP_1 && op <= doge.OP_3
}

func decodeOP_N(op byte) int {
	return int(op - (doge.OP_1 - 1)) // same as (op - doge.OP_1) + 1
}
