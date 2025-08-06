package spec

type UTXO struct {
	TxID   []byte // 32-byte tx hash
	VOut   uint32 // tx output index
	Value  int64  // Koinu value
	Type   byte   // script type
	Script []byte // content depends on 'Type' (compressed by ClassifyScript)
}
