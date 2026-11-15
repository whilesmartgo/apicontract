# Changelog

## 0.1.1 (2026-11-15)

- Parse list and search query parameters into typed structs. Support day and timestamp ranges, limit clamping and declared deprecated aliases. Reject undeclared parameters with `422` unless a malformed input takes precedence. Reject repeated declared parameters with `400`.
- Parse JSON request bodies into structs using the same rule tags. Support nested objects, lists, nullable optional fields and deprecated field names. Enforce a size limit and reject unknown struct fields.
- Report errors as RFC 9457 problem details with `application/problem+json` and a stable `code`. Include field errors in `errors` when available. Return `400` for malformed input and `422` for rule violations when no malformed input is found.
- Read and validate `Idempotency-Key` as a quoted string or a bare key.
- Use `Date` for calendar days in queries and bodies.
- Sign cursors with request bindings that include the method, escaped path, query and supplied caller identifiers. Exclude the cursor and page limit from the query binding.
- Generate JSON Schema for tools and OpenAPI parameters and request bodies from query and body structs. Describe problem responses too.
- Check OpenAPI documents against the contract, allowing recorded exceptions. Fail on violations without exceptions and stale exceptions for checked documents.
- Link each built-in problem code's `type` to its documentation section. Check README example outputs in tests.

## 0.1.0 (2026-11-15)

Retracted. Use 0.1.1.
