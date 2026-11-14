# API contract for Workspace apps

Shared rules and tools for naming, parsing and paging HTTP queries in Workspace apps.

Clients, MCP tools and agents need to handle each app's date ranges, paging and errors. Shared names and formats let them reuse that handling. This module provides a query parser, a list response body and an error body. Its command checks `openapi.json` from apps written in any language.

Contract version: **0.1**. It changes with this module's minor version.

## The rules

| Area | Rule |
| --- | --- |
| Names | Query parameters and JSON fields are snake_case. Free-text search is `q`. |
| Instants | RFC 3339 with an offset, such as `2026-11-14T09:00:00+01:00`. |
| Days | `YYYY-MM-DD`, resolved in the `tz` parameter (an IANA zone), or UTC. |
| Ranges | `from` and `to`, or `<field>_from` and `<field>_to` when a resource has several time fields. Both accept a timestamp or a day. `from` is inclusive. A timestamp in `to` is exclusive; a day in `to` includes that whole day. |
| Paging | `limit` defaults to 50; values above 200 return 200. `cursor` is opaque and bound to the query and caller through `Cursors` and `QueryBinding`. The binding excludes `cursor` and `limit`. |
| Lists | `{"items": [...], "next_cursor": "..."}`. `next_cursor` is absent on the last page. `total` appears only when it is exact and cheap. |
| Sorting | `sort=created_at,-title`. A leading `-` sorts descending. |
| Errors | `{"error": "message", "code": "snake_case", "fields": [...]}`. `fields` is omitted when empty. Query parsing returns 400 for malformed values and 422 for rule violations when no value is malformed. |
| Old names | Keep `page`, `per_page`, `page_size`, `offset`, `start`, `end`, `query` and `search` accepted while callers move, and mark them deprecated in the spec. Routes must declare the old parameters; the parser does not add them. |
| Version | `info.x-whilesmart-contract` in the OpenAPI document states the version the app meets. |

The JSON Schemas in [`schema/`](schema) are the machine-readable form: shared query parameters, the list envelope and the error body.

## Parse a list query

Declare the parameters a route takes as a struct. Embed the shared pieces and add your own fields with `query` tags.

```go
type eventsQuery struct {
	apicontract.Page
	apicontract.Range
	apicontract.Zone
	CalendarID string                `query:"calendar_id"`
	Sort       []apicontract.SortKey `query:"sort" enum:"starts_at,title"`
	Start      time.Time             `query:"start" deprecated:"from"`
}

func listEvents(w http.ResponseWriter, r *http.Request) {
	var q eventsQuery
	if err := apicontract.BindQuery(r, &q); err != nil {
		apicontract.WriteProblem(w, err)
		return
	}
	// q.Limit is between 1 and 200, q.From and q.To are a half-open range.
	apicontract.WriteList(w, events, next)
}
```

With Gin, pass `c.Request` and `c.Writer`.

A field tagged `deprecated:"from"` accepts the old name and fills the declared `from` field when the caller leaves it empty or omits it. Different values, compared as trimmed strings, produce a conflict. The response is 422 unless another value is malformed, which makes it 400.

Name a range field to prefix its parameters. This field reads `updated_from` and `updated_to`:

```go
Updated apicontract.Range `query:"updated"`
```

Supported tags: `query`, `doc`, `default`, `required`, `minimum`, `maximum`, `clamp`, `maxLength`, `enum`, `deprecated` and `bound:"end"`.

## Cursors

`Cursors` signs a page position and binds it to a string supplied by the app. Use `QueryBinding` with the query and caller IDs to reject a cursor from another caller or query with a 400. It excludes `cursor` and `limit`, so callers can change the page size.

```go
cursors, err := apicontract.NewCursors(key) // at least 32 bytes
binding := apicontract.QueryBinding(r.URL.Query(), userID, ownerID)
next, err := cursors.Encode(binding, position)
err = cursors.Decode(q.Cursor, binding, &position)
```

Positions are signed, not encrypted. Do not put secrets in them.

## Describe the same parameters elsewhere

- `InputSchema(eventsQuery{})` returns a JSON Schema object for tool inputs, such as MCP tools. It describes the route's parameter names and bounds; it does not parse inputs or apply aliases and clamping.
- `OpenAPIParameters(eventsQuery{})` returns OpenAPI 3.1 query parameter objects for specs built in code.

## Check an OpenAPI document

The `apicontract` command checks contract rules in OpenAPI 3.0 and 3.1 JSON documents from any language. It follows local references, up to 16 steps, but does not resolve external references or validate the full OpenAPI specification. It checks GET, PUT, POST, PATCH and DELETE operations.

```sh
go run github.com/whilesmartgo/apicontract/cmd/apicontract@latest lint openapi.json
```

It reports:

| Rule | Fails when |
| --- | --- |
| `contract-version` | `info.x-whilesmart-contract` is missing, empty or not a string. |
| `param-name` | A query parameter is not snake_case. |
| `param-legacy` | An old name such as `page_size` is not marked deprecated. |
| `param-time-format` | A parameter named `from`, `to`, `since` or `until`, or ending in `_from`, `_to`, `_since` or `_until`, has no `date` or `date-time` format, directly or in `anyOf` or `oneOf`. |
| `response-schema` | A GET route declares a 200 response with no content, or with `application/json` but no nonempty schema. |
| `list-envelope` | A collection's 200 response schema has no `items` array. This check does not require `next_cursor`. |
| `list-limit` | A collection has no `limit`, or `limit` is not an integer with a declared maximum of at most 200. |
| `list-cursor` | A collection has no `cursor`. |
| `error-responses` | A checked operation with query parameters or a body declares none of 400, 422 or `4XX`. |

A GET route is treated as a collection when its 200 `application/json` schema is an array or has an array in `items` or `data`, or when it declares `limit`, `cursor`, `page`, `per_page`, `page_size` or `offset`.

### Adopt it on an existing API

Record current violations with a reason, then run the check in CI:

```sh
apicontract lint -exceptions api-contract.exceptions.json -seed "predates the contract" openapi.json
apicontract lint -exceptions api-contract.exceptions.json openapi.json
```

Violations that match no exception fail the run. An exception that matches no violation in a checked document also fails. An entry may use `*` for `rule`, `method` or `path`, and may omit `subject` to cover every parameter. Broad exceptions can accept new violations. Running `-seed` again adds exceptions for current violations and removes stale ones in checked documents. `-report file.md` writes accepted violations as a Markdown table.

## License

MIT
