package apicontract

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Error codes written in ErrorBody.Code.
const (
	CodeMalformedQuery = "malformed_query"
	CodeInvalidQuery   = "invalid_query"
	CodeInternal       = "internal_error"
)

// Field error codes written in FieldError.Code.
const (
	FieldInvalid       = "invalid"
	FieldRequired      = "required"
	FieldOutOfRange    = "out_of_range"
	FieldTooLong       = "too_long"
	FieldUnknownValue  = "unknown_value"
	FieldConflict      = "conflict"
	FieldInvalidCursor = "invalid_cursor"
)

type FieldError struct {
	Name    string `json:"name"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ErrorBody is the JSON body of every error response. Error stays a plain
// string so clients that read it as a message keep working.
type ErrorBody struct {
	Error  string       `json:"error"`
	Code   string       `json:"code"`
	Fields []FieldError `json:"fields,omitempty"`
}

// Problem is an error that knows its HTTP status and body.
type Problem struct {
	Status  int
	Code    string
	Message string
	Fields  []FieldError
}

func (p *Problem) Error() string { return p.Message }

func (p *Problem) Body() ErrorBody {
	return ErrorBody{Error: p.Message, Code: p.Code, Fields: p.Fields}
}

// WriteJSON writes value as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, value any) error {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if status == http.StatusNoContent {
		return nil
	}
	return json.NewEncoder(w).Encode(value)
}

// WriteError writes an error body with the given status.
func WriteError(w http.ResponseWriter, status int, code, message string, fields ...FieldError) error {
	return WriteJSON(w, status, ErrorBody{Error: message, Code: code, Fields: fields})
}

// WriteProblem writes err as an error body. Errors that are not a *Problem are
// written as a 500 without exposing their text.
func WriteProblem(w http.ResponseWriter, err error) error {
	var problem *Problem
	if errors.As(err, &problem) {
		return WriteJSON(w, problem.Status, problem.Body())
	}
	return WriteError(w, http.StatusInternalServerError, CodeInternal, "Internal server error")
}
