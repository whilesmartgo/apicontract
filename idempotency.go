package apicontract

import (
	"net/http"
	"strings"
)

// IdempotencyHeader is the request header that carries an idempotency key.
const IdempotencyHeader = "Idempotency-Key"

const (
	minIdempotencyKey = 8
	maxIdempotencyKey = 128
)

// IdempotencyKey returns the request's Idempotency-Key, or an empty string when
// the header is absent. The key is a Structured Field String as the IETF draft
// specifies, such as "8e03978e-40d5-43e8-bc93-6894a57f9324" with the quotes, or
// a bare key of visible ASCII without commas, quotes or backslashes. The
// decoded key must be 8 to 128 characters. A key sent more than once, in
// separate fields or joined by a comma, or one that cannot be parsed returns a
// *Problem with status 400. Storing keys and replaying responses is left to the
// app.
func IdempotencyKey(r *http.Request) (string, error) {
	values := r.Header.Values(IdempotencyHeader)
	if len(values) == 0 {
		return "", nil
	}
	invalid := func(detail string) error {
		return NewProblem(http.StatusBadRequest, CodeMalformedHeader, "", FieldError{In: InHeader, Name: IdempotencyHeader, Code: FieldInvalid, Detail: detail})
	}
	if len(values) > 1 {
		return "", invalid("must be sent once")
	}
	raw := strings.Trim(values[0], " \t")
	var key string
	if strings.HasPrefix(raw, `"`) {
		decoded, ok := parseStructuredString(raw)
		if !ok {
			return "", invalid("must be one quoted string or one bare key")
		}
		key = decoded
	} else {
		for i := 0; i < len(raw); i++ {
			if raw[i] < 0x21 || raw[i] > 0x7e || raw[i] == '"' || raw[i] == '\\' || raw[i] == ',' {
				return "", invalid("must be one quoted string or one bare key of visible ASCII without commas")
			}
		}
		key = raw
	}
	if len(key) < minIdempotencyKey || len(key) > maxIdempotencyKey {
		return "", invalid("must be 8 to 128 characters")
	}
	return key, nil
}

// parseStructuredString decodes an RFC 9651 sf-string that makes up the whole
// value: printable ASCII between double quotes, with \" and \\ as the only
// escapes.
func parseStructuredString(raw string) (string, bool) {
	if len(raw) < 2 || raw[0] != '"' {
		return "", false
	}
	var out strings.Builder
	for i := 1; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c == '\\':
			i++
			if i >= len(raw) || (raw[i] != '"' && raw[i] != '\\') {
				return "", false
			}
			out.WriteByte(raw[i])
		case c == '"':
			return out.String(), i == len(raw)-1
		case c < 0x20 || c > 0x7e:
			return "", false
		default:
			out.WriteByte(c)
		}
	}
	return "", false
}
