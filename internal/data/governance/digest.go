package governance

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

func ComputeDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func digestFields(fields ...string) string {
	h := sha256.New()
	for _, f := range fields {
		var l [4]byte
		binary.BigEndian.PutUint32(l[:], uint32(len(f)))
		h.Write(l[:])
		h.Write([]byte(f))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func ComputeInputDigest(canonical []byte) string {
	return ComputeDigest(canonical)
}

func ComputeResultDigest(canonical []byte) string {
	return ComputeDigest(canonical)
}

func ComputeCanonicalDigest(fields ...string) string {
	return digestFields(fields...)
}
