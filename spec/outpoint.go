package spec

import "encoding/binary"

type OutPointKey []byte // always 36 bytes

// OutPoint makes a binary string to use as a database key.
func OutPoint(txID []byte, index uint32) OutPointKey {
	point := make([]byte, 32, 36)
	copy(point[0:32], txID[0:32]) // always 32 bytes
	return binary.BigEndian.AppendUint32(point, index)
}
