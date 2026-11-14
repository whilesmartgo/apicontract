# API contract for Workspace apps

One set of rules for how Workspace apps name, parse and page their HTTP queries, and the tools that make following them the easy path.

Clients, MCP tools and agents talk to many apps. When each app names its date range, paging and errors differently, every client has to learn each app separately. This module gives apps one parser, one list envelope and one error body, and gives any app, in any language, a check it can run against its own `openapi.json`.

Contract version: **0.1**. It changes with this module's minor version.

## The rules

| Area | Rule |
| --- | --- |
| Names | Query parameters and JSON fields are snake_case. Free-text search is `q`. |
| Instants | RFC 3339 with an offset, such as `2026-11-14T09:00:00+01:00`. |
| Days | `YYYY-MM-DD`, resolved in the `tz` parameter (an IANA zone), or UTC. |
| Ranges | `from` and `to`, or `<field>_from` and `<field>_to` when a resource has several time fields. Both accept a timestamp or a day. `from` is inclusive. A timestamp in `to` is exclusive; a day in `to` includes that whole day. |
| Paging | `limit` defaults to 50; values above 200 return 200. `cursor` is opaque and valid only for the same query and caller. |
| Lists | `{"items": [...], "next_cursor": "..."}`. `next_cursor` is absent on the last page. `total` appears only when it is exact and cheap. |
| Sorting | `sort=created_at,-title`. A leading `-` sorts descending. |
| Errors | `{"error": "message", "code": "snake_case", "fields": [...]}`. 400 when the request cannot be parsed; 422 when it parses but breaks a rule. |
| Old names | `page`, `per_page`, `page_size`, `offset`, `start`, `end`, `query` and `search` stay accepted while callers move, and the spec marks them deprecated. |
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

A field tagged `deprecated:"from"` accepts the old name and fills `from` when the caller did not send it. Sending both with different values is a 422.

Name a range field to prefix its parameters. This field reads `updated_from` and `updated_to`:

```go
Updated apicontract.Range `query:"updated"`
```

Supported tags: `query`, `doc`, `default`, `required`, `minimum`, `maximum`, `clamp`, `maxLength`, `enum`, `deprecated` and `bound:"end"`.

## Cursors

`Cursors` signs a page position and binds it to the query and the caller, so a cursor replayed by another user or with other filters is a 400.

```go
cursors, err := apicontract.NewCursors(key) // at least 32 bytes
binding := apicontract.QueryBinding(r.URL.Query(), userID, ownerID)
next, err := cursors.Encode(binding, position)
err = cursors.Decode(q.Cursor, binding, &position)
```

Positions are signed, not encrypted. Do not put secrets in them.

## Describe the same parameters elsewhere

- `InputSchema(eventsQuery{})` returns a JSON Schema object for tool inputs, such as MCP tools, so tools accept the same names and bounds as the route.
- `OpenAPIParameters(eventsQuery{})` returns OpenAPI query parameter objects for specs built in code.

## Check an OpenAPI document

The `apicontract` command checks any OpenAPI 3.0 or 3.1 JSON document, whatever language produced it.

```sh
go run github.com/whilesmartgo/apicontract/cmd/apicontract@latest lint openapi.json
```

It reports:

| Rule | Fails when |
| --- | --- |
| `contract-version` | `info.x-whilesmart-contract` is missing. |
| `param-name` | A query parameter is not snake_case. |
| `param-legacy` | An old name such as `page_size` is not marked deprecated. |
| `param-time-format` | A range parameter does not declare `format: date` or `date-time`. |
| `response-schema` | A GET route does not declare its JSON response schema. |
| `list-envelope` | A collection does not return `items` and `next_cursor`. |
| `list-limit` | A collection has no `limit`, or its maximum is above 200. |
| `list-cursor` | A collection has no `cursor`. |
| `error-responses` | A route with query parameters or a body declares no 400 or 422. |

A GET route is a collection when its response is an array or holds one in `items` or `data`, or when it takes a paging parameter.

### Adopt it on an existing API

Record today's violations once, each with a reason, then keep the check in CI:

```sh
apicontract lint -exceptions api-contract.exceptions.json -seed "predates the contract" openapi.json
apicontract lint -exceptions api-contract.exceptions.json openapi.json
```

New violations fail the run. So does an exception that no longer matches anything, so the list only shrinks. An entry may use `*` for `rule`, `method` or `path`, and may leave out `subject` to cover every parameter. `-report file.md` writes the accepted violations as a Markdown table.

## License

MIT
