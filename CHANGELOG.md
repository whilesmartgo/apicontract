# Changelog

## 0.2.0 (unreleased)

Changed:

- Error responses are RFC 9457 problem details sent as `application/problem+json`. Responses include `title`, `status` and `code`; `detail`, `type` and `errors` are optional. Field errors use `in`, `name`, `code` and `detail`.
- Undeclared query parameters are rule violations (`422` unless another input is malformed). Declared parameters sent twice return `400`.
- `maxLength` counts characters, not bytes, and applies to each entry of a string list.
- Timestamp parsing rejects one-digit hours, comma fractions and offsets with hours above 23 or minutes above 59.
- A `Cursors` value not made by `NewCursors` refuses to sign or verify.
- Invalid declared defaults and invalid `MaxBytes` values are programming errors.
- `list-envelope` requires `items` to be required and not nullable, and `error-media-type` covers every declared 4xx and 5xx response. Lint checks `head`, `options` and `trace` operations, reads keywords beside a `$ref` together with the referenced schema, finds lists inside `allOf`, `anyOf` and `oneOf`, and requires `date-time` for body fields ending in `_at`.
- Contract version 0.2.

Added:

- Parse JSON request bodies into structs with the same rule tags as queries, including nested objects, lists, optional pointers that accept `null`, deprecated field names and a size limit. Unknown fields are rejected.
- Read and check `Idempotency-Key`, as a quoted string or a bare key.
- `RequestBinding` binds a cursor to the method, escaped path, query and caller.
- Schemas carry the range of sized integers and exact integer bounds, and a required field with an old name is satisfied by either name with a value.
- Optional `tz` parameters take defaults, and required ones are enforced.
- `Date` for calendar days, in queries and bodies.
- Describe body structs as JSON Schema and OpenAPI request bodies, and describe the problem response.
- Lint rules `error-media-type`, `body-name`, `body-time-format` and `body-closed`.
- A documentation section for each built-in problem code, linked from its `type`.
- README examples whose outputs are checked by the tests.

## 0.1.0 (2026-11-15)

- Parse list and search query parameters into typed structs, with days and timestamps in ranges, clamped limits and declared deprecated aliases.
- Write the list envelope and the error body, with 400 for malformed query values and 422 for rule violations when no value is malformed.
- Sign cursors with an app-supplied binding. Build query and caller bindings that allow page size changes.
- Describe the same parameters as JSON Schema for tools and as OpenAPI parameters.
- Check OpenAPI documents against the contract, with recorded exceptions. Fail on unmatched violations and stale exceptions in checked documents.
