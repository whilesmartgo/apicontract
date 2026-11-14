package apicontract

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// Cursors signs and verifies opaque cursors. A cursor carries a JSON position
// and a MAC over the position and a binding, so it is rejected when replayed
// against a different query or principal. Positions are readable by anyone who
// decodes the cursor; do not put secrets in them.
type Cursors struct {
	key []byte
}

// NewCursors returns a Cursors signing with key, which must be at least 32 bytes.
func NewCursors(key []byte) (*Cursors, error) {
	if len(key) < 32 {
		return nil, errors.New("apicontract: cursor key must be at least 32 bytes")
	}
	return &Cursors{key: append([]byte(nil), key...)}, nil
}

// Encode returns a cursor for position, bound to binding.
func (c *Cursors) Encode(binding string, position any) (string, error) {
	payload, err := json.Marshal(position)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(c.mac(binding, payload))
	if len(encoded) > MaxCursorLength {
		return "", errors.New("apicontract: cursor position is too large")
	}
	return encoded, nil
}

// Decode verifies cursor against binding and decodes its position. A cursor
// that is malformed, tampered with or bound to something else returns a
// *Problem with status 400.
func (c *Cursors) Decode(cursor, binding string, position any) error {
	invalid := &Problem{Status: http.StatusBadRequest, Code: CodeMalformedQuery, Message: "The query string could not be parsed.", Fields: []FieldError{{Name: "cursor", Code: FieldInvalidCursor, Message: "is not a cursor issued for this query"}}}
	if len(cursor) > MaxCursorLength {
		return invalid
	}
	body, signature, ok := strings.Cut(cursor, ".")
	if !ok {
		return invalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return invalid
	}
	mac, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || !hmac.Equal(mac, c.mac(binding, payload)) {
		return invalid
	}
	if err := json.Unmarshal(payload, position); err != nil {
		return invalid
	}
	return nil
}

func (c *Cursors) mac(binding string, payload []byte) []byte {
	hash := hmac.New(sha256.New, c.key)
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(binding)))
	hash.Write(length[:])
	hash.Write([]byte(binding))
	hash.Write(payload)
	return hash.Sum(nil)
}

// QueryBinding returns a binding for a cursor from the query values that shape
// the result and from the principal parts, such as user and owner IDs. cursor
// and limit are left out so a client can change the page size between pages.
func QueryBinding(values url.Values, principal ...string) string {
	shaped := url.Values{}
	for key, list := range values {
		if key != "cursor" && key != "limit" {
			shaped[key] = list
		}
	}
	hash := sha256.New()
	parts := append(append([]string(nil), principal...), shaped.Encode())
	for _, part := range parts {
		var length [8]byte
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		hash.Write(length[:])
		hash.Write([]byte(part))
	}
	return hex.EncodeToString(hash.Sum(nil))
}
