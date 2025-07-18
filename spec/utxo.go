package spec

type UTXO struct {
	Key    OutPointKey
	Value  int64
	Type   byte   // script type
	Script []byte // content depends on Script[0] (OutType)
}
