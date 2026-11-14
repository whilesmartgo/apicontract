# Changelog

## 0.1.0 (unreleased)

- Parse list and search query parameters into typed structs, with days and timestamps in ranges, clamped limits and declared deprecated aliases.
- Write the list envelope and the error body, with 400 for malformed query values and 422 for rule violations when no value is malformed.
- Sign cursors with an app-supplied binding. Build query and caller bindings that allow page size changes.
- Describe the same parameters as JSON Schema for tools and as OpenAPI parameters.
- Check OpenAPI documents against the contract, with recorded exceptions. Fail on unmatched violations and stale exceptions in checked documents.
