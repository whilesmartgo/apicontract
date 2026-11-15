package apicontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
)

// ProblemContentType is the media type of every error response (RFC 9457).
const ProblemContentType = "application/problem+json"

// ProblemTypeBase prefixes the type URI of the problems this package writes.
// Each code has a section in docs/errors.md.
const ProblemTypeBase = "https://github.com/whilesmartgo/apicontract/blob/main/docs/errors.md#"

// Problem codes written in Problem.Code.
const (
	CodeMalformedQuery       = "malformed_query"
	CodeInvalidQuery         = "invalid_query"
	CodeMalformedBody        = "malformed_body"
	CodeInvalidBody          = "invalid_body"
	CodeUnsupportedMediaType = "unsupported_media_type"
	CodeBodyTooLarge         = "body_too_large"
	CodeMalformedHeader      = "malformed_header"
	CodeInternal             = "internal_error"
)

var titles = map[string]string{
	CodeMalformedQuery:       "The query string could not be parsed.",
	CodeInvalidQuery:         "The query string is not valid.",
	CodeMalformedBody:        "The request body could not be parsed.",
	CodeInvalidBody:          "The request body is not valid.",
	CodeUnsupportedMediaType: "The request body must be JSON.",
	CodeBodyTooLarge:         "The request body is too large.",
	CodeMalformedHeader:      "A request header could not be parsed.",
	CodeInternal:             "Internal server error.",
}

// Field error codes written in FieldError.Code.
const (
	FieldInvalid       = "invalid"
	FieldRequired      = "required"
	FieldOutOfRange    = "out_of_range"
	FieldTooLong       = "too_long"
	FieldTooMany       = "too_many"
	FieldUnknownValue  = "unknown_value"
	FieldUnknown       = "unknown_field"
	FieldConflict      = "conflict"
	FieldInvalidCursor = "invalid_cursor"
)

// Where a FieldError points, written in FieldError.In.
const (
	InQuery  = "query"
	InBody   = "body"
	InHeader = "header"
)

// FieldError is one input at fault. Name is the query parameter, the header,
// or the body field path as the client sent it, such as attendees[2].email.
type FieldError struct {
	In     string `json:"in"`
	Name   string `json:"name"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

// Problem is an error that is written as an RFC 9457 problem details object.
// Code and Errors are extension members.
type Problem struct {
	Type   string       `json:"type,omitempty"`
	Title  string       `json:"title"`
	Status int          `json:"status"`
	Detail string       `json:"detail,omitempty"`
	Code   string       `json:"code"`
	Errors []FieldError `json:"errors,omitempty"`
}

// NewProblem returns a problem with status and code. Codes defined by this
// package get their documented type and title; other codes get the status
// text as title and no type, which RFC 9457 reads as about:blank.
func NewProblem(status int, code, detail string, errors ...FieldError) *Problem {
	problem := &Problem{Title: http.StatusText(status), Status: status, Detail: detail, Code: code, Errors: errors}
	if title, ok := titles[code]; ok {
		problem.Type, problem.Title = ProblemTypeBase+code, title
	}
	return problem
}

func (p *Problem) Error() string {
	if p.Detail != "" {
		return p.Detail
	}
	return p.Title
}

// WriteJSON writes value as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, value any) error {
	return write(w, status, "application/json", value)
}

// write encodes value before sending the status, so an encoding failure
// leaves the response unwritten and the caller can still send a problem.
func write(w http.ResponseWriter, status int, contentType string, value any) error {
	if status < 200 || status == http.StatusNoContent || status == http.StatusResetContent || status == http.StatusNotModified {
		w.WriteHeader(status)
		return nil
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(value); err != nil {
		return err
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_, err := w.Write(body.Bytes())
	return err
}

// WriteError writes a problem with status, code and detail.
func WriteError(w http.ResponseWriter, status int, code, detail string, errors ...FieldError) error {
	return WriteProblem(w, NewProblem(status, code, detail, errors...))
}

// WriteProblem writes err as a problem. Errors that are not a *Problem are
// written as a 500 without exposing their text.
func WriteProblem(w http.ResponseWriter, err error) error {
	var problem *Problem
	if !errors.As(err, &problem) {
		problem = NewProblem(http.StatusInternalServerError, CodeInternal, "")
	}
	return write(w, problem.Status, ProblemContentType, problem)
}
