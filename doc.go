// Package apicontract implements the shared HTTP API contract for Workspace apps.
//
// It parses list and search query parameters into typed structs, writes the
// list envelope and the error body, signs opaque cursors, and describes the
// same parameters as JSON Schema and OpenAPI so specs and tool schemas match
// the parser.
package apicontract
