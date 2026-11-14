package apicontract

import "net/http"

// List is the response envelope for collection routes. NextCursor is empty on
// the last page. Total is set only when it is exact and cheap to compute.
type List[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
	Total      *int   `json:"total,omitempty"`
}

// NewList returns a List whose Items encode as [] rather than null.
func NewList[T any](items []T, next string) List[T] {
	if items == nil {
		items = []T{}
	}
	return List[T]{Items: items, NextCursor: next}
}

// WriteList writes a 200 response with the list envelope.
func WriteList[T any](w http.ResponseWriter, items []T, next string) error {
	return WriteJSON(w, http.StatusOK, NewList(items, next))
}
