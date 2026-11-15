// Package apicontract implements the shared HTTP API contract for Workspace apps.
//
// It parses query strings and JSON request bodies into typed structs, rejects
// input a route does not declare, writes the list envelope and RFC 9457
// problem bodies, signs opaque cursors, and describes the same structs as JSON
// Schema and OpenAPI so specs and tool schemas match the parser.
package apicontract
