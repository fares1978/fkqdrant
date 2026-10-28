package fkqdrant

import (
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"strconv"

	"github.com/qdrant/go-client/qdrant"
)

// IDNamespace is the UUID namespace used by IDFrom. It never changes, so
// other languages can derive the same IDs, e.g. in Python:
// uuid.uuid5(uuid.UUID(IDNamespace), s).
const IDNamespace = "3f1d6c2e-8b4a-4e57-9a0c-5d2f7b1e9c84"

var idNamespace = mustParseUUID(IDNamespace)

// NewID returns a random (version 4) UUID string to use as a point ID.
func NewID() string {
	var u [16]byte
	_, _ = rand.Read(u[:])
	u[6] = u[6]&0x0f | 0x40
	u[8] = u[8]&0x3f | 0x80
	return formatUUID(u)
}

// IDFrom returns a deterministic (version 5) UUID string derived from s, so
// the same key always maps to the same point ID. Use it for keys that are
// not UUIDs, such as a MongoDB ObjectID hex or "docID:chunkIndex".
func IDFrom(s string) string {
	h := sha1.New()
	h.Write(idNamespace[:])
	h.Write([]byte(s))
	var u [16]byte
	copy(u[:], h.Sum(nil))
	u[6] = u[6]&0x0f | 0x50
	u[8] = u[8]&0x3f | 0x80
	return formatUUID(u)
}

func formatUUID(u [16]byte) string {
	var b [36]byte
	hex.Encode(b[0:8], u[0:4])
	b[8] = '-'
	hex.Encode(b[9:13], u[4:6])
	b[13] = '-'
	hex.Encode(b[14:18], u[6:8])
	b[18] = '-'
	hex.Encode(b[19:23], u[8:10])
	b[23] = '-'
	hex.Encode(b[24:], u[10:])
	return string(b[:])
}

func mustParseUUID(s string) [16]byte {
	var u [16]byte
	clean := s[0:8] + s[9:13] + s[14:18] + s[19:23] + s[24:]
	if _, err := hex.Decode(u[:], []byte(clean)); err != nil {
		panic(err)
	}
	return u
}

// pointID converts an ID string to a Qdrant point ID. Decimal strings map to
// numeric IDs, so collections written by other tools can be read back.
func pointID(id string) *qdrant.PointId {
	if n, err := strconv.ParseUint(id, 10, 64); err == nil {
		return qdrant.NewIDNum(n)
	}
	return qdrant.NewIDUUID(id)
}

func pointIDs(ids []string) []*qdrant.PointId {
	out := make([]*qdrant.PointId, len(ids))
	for i, id := range ids {
		out[i] = pointID(id)
	}
	return out
}

// idString is the inverse of pointID.
func idString(id *qdrant.PointId) string {
	if u := id.GetUuid(); u != "" {
		return u
	}
	return strconv.FormatUint(id.GetNum(), 10)
}
