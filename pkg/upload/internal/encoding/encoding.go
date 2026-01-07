package encoding

import (
	"github.com/ipfs/go-cid"
	_ "github.com/ipld/go-ipld-prime/codec/dagjson"
	_ "github.com/ipld/go-ipld-prime/codec/raw"
)

// ToV1 converts a CID to version 1 format if it's version 0
// Returns CID unchanged if already version 1, or CID.Undef for unsupported versions
func ToV1(c cid.Cid) cid.Cid {
	switch c.Version() {
	case 0:
		newCid := cid.NewCidV1(c.Type(), c.Hash())
		return newCid
	case 1:
		// Already v1 - return as-is
		return c
	default:
		// Unsupported version
		return cid.Undef
	}
}

// NormalizeCid ensures a CID is in version 1 format
// This is used to maintain consistent CID representations across the system
func NormalizeCid(c cid.Cid) cid.Cid {
	return ToV1(c)
}
