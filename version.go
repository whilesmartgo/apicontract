package apicontract

// Version is the contract version this module implements. An OpenAPI document
// states the version it meets in info.x-whilesmart-contract.
const Version = "0.2"

// VersionExtension is the OpenAPI info extension that carries Version.
const VersionExtension = "x-whilesmart-contract"

const (
	DefaultLimit    = 50
	MaxLimit        = 200
	MaxCursorLength = 2048
)
