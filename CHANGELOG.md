# Changelog

## 0.1.0 (unreleased)

- Parse list and search query parameters into typed structs, with days and timestamps in ranges, clamped limits and deprecated aliases.
- Write the list envelope and the error body, with 400 for malformed input and 422 for broken rules.
- Sign cursors bound to the query and the caller.
- Describe the same parameters as JSON Schema for tools and as OpenAPI parameters.
- Check OpenAPI documents against the contract, with recorded exceptions that may only shrink.
