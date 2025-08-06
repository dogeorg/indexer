package spec

type OutPointKey struct {
	Tx   []byte // 32-byte hash
	VOut uint32 // output number
}

// OutPoint makes a binary string to use as a database key.
func OutPoint(txID []byte, index uint32) OutPointKey {
	return OutPointKey{Tx: txID, VOut: index}
}
