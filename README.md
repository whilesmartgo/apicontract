# API contract for Workspace apps

Shared rules and tools for query strings, request bodies, paging and errors in Workspace app APIs.

Apps that share parameter names, paging and errors can share client code. This module gives Go apps a parser for query strings and JSON bodies, the list response, RFC 9457 error bodies and signed cursors. Its command checks the `openapi.json` of an app written in any language.

Contract version: **0.2**. The package exposes this version as `Version`.

## Standards it builds on

| Topic | Source |
| --- | --- |
| Methods, status codes, `413`, `415`, `422` | RFC 9110, HTTP Semantics |
| Timestamps | RFC 3339 |
| Error bodies | RFC 9457, Problem Details for HTTP APIs |
| Time zones | IANA time zone database |
| Idempotency keys | IETF draft `draft-ietf-httpapi-idempotency-key-header` |
| Parameter names, cursor paging, snake_case | Zalando RESTful API Guidelines |

The contract departs from the Zalando guidelines in two places. Requests that parse but break a rule get `422`, as RFC 9110 defines it; Zalando sends `400` for both. Request schemas set `additionalProperties: false` and the parser rejects fields it does not know; Zalando asks servers to report unknown input but asks schemas to stay open.

## The rules

| Area | Rule |
| --- | --- |
| Names | Query parameters and JSON fields are snake_case. Free-text search is `q`. |
| Unknown input | Undeclared query parameters and body fields are rule violations (`422` unless another input is malformed). A declared query parameter sent twice is malformed (`400`). |
| Instants | RFC 3339 with upper-case `T` and either a numeric offset or `Z`, such as `2026-11-14T09:00:00+01:00`. |
| Days | `YYYY-MM-DD`. In a query range, a day is read in the `tz` parameter (an IANA zone), or UTC. |
| Ranges | `from` and `to`, or `<field>_from` and `<field>_to` when a resource has several time fields. Both accept a timestamp or a day. `from` is inclusive. A timestamp in `to` is exclusive; a day in `to` includes that whole day. |
| Paging | `limit` defaults to 50; values above 200 return 200. Use the cursor helpers to bind `cursor` to the query and caller. |
| Lists | `{"items": [...], "next_cursor": "..."}`. `next_cursor` is omitted when empty. Set `total` only when it is exact and cheap to compute. |
| Sorting | `sort=created_at,-title`. A leading `-` sorts descending. |
| Bodies | JSON objects sent as `application/json`, at most 1 MiB unless the route says otherwise. |
| Errors | RFC 9457 problem details as `application/problem+json`, with a stable `code` and an optional `errors` list. `400` when input cannot be parsed, `422` when it parses but breaks a rule, `413` for a body that is too large, `415` for a missing or unsupported content type. |
| Retries | Creating requests may send `Idempotency-Key`: a quoted string as the IETF draft defines it, or a bare key of visible ASCII without commas, quotes or backslashes. The decoded key is 8 to 128 characters. |
| Old names | Keep `page`, `per_page`, `page_size`, `offset`, `start`, `end`, `query` and `search` accepted while callers move, and mark them deprecated in the spec. |
| Version | `info.x-whilesmart-contract` in the OpenAPI document states the version the app meets. |

The JSON Schemas in [`schema/`](schema) are the machine-readable form: shared query parameters, the list envelope and the problem body. [`docs/errors.md`](docs/errors.md) explains each problem `code`; problems with built-in codes link there through `type`.

## Examples

The examples have outputs checked by `go test`.

### Page through a list

Declare the parameters a route takes as a struct. Embed the shared pieces and add your own fields with `query` tags.

```go
type notesList struct {
	apicontract.Page
	apicontract.Search
}

func listNotes(w http.ResponseWriter, r *http.Request) {
	var q notesList
	if err := apicontract.BindQuery(r, &q); err != nil {
		apicontract.WriteProblem(w, err)
		return
	}
	binding := apicontract.RequestBinding(r, userID)
	var after struct{ ID int `json:"id"` }
	if q.Cursor != "" {
		if err := cursors.Decode(q.Cursor, binding, &after); err != nil {
			apicontract.WriteProblem(w, err)
			return
		}
	}
	notes, last := loadNotesAfter(after.ID, q.Limit)
	next := ""
	if !last {
		next, _ = cursors.Encode(binding, map[string]int{"id": notes[len(notes)-1].ID})
	}
	apicontract.WriteList(w, notes, next)
}
```

`GET /notes?limit=2`, then the same request with the returned cursor, then that cursor with a different query (`q=budget` added):

```text
200 application/json
{
  "items": [
    {
      "id": 1,
      "title": "Budget"
    },
    {
      "id": 2,
      "title": "Roadmap"
    }
  ],
  "next_cursor": "eyJpZCI6Mn0.cuGm7m76S3bOizAsEbyVpHEsw-5bafeEGFHJ26VFq-E"
}
200 application/json
{
  "items": [
    {
      "id": 3,
      "title": "Hiring"
    }
  ]
}
400 application/problem+json
{
  "type": "https://github.com/whilesmartgo/apicontract/blob/main/docs/errors.md#malformed_query",
  "title": "The query string could not be parsed.",
  "status": 400,
  "code": "malformed_query",
  "errors": [
    {
      "in": "query",
      "name": "cursor",
      "code": "invalid_cursor",
      "detail": "is not a cursor issued for this query"
    }
  ]
}
```

`next_cursor` is absent on the last page. `RequestBinding` ties a cursor to the method, escaped path, query and caller it was issued for, so it fails on another route, another resource or another filter; `limit` may change between pages.

### Read a day range in a time zone

```go
var q struct {
	apicontract.Range
	apicontract.Zone
}
// GET /events?from=2026-11-14&to=2026-11-14&tz=Europe/Berlin
apicontract.BindQuery(r, &q)
fmt.Println(q.From.Format(time.RFC3339))
fmt.Println(q.To.Format(time.RFC3339))
```

```text
2026-11-14T00:00:00+01:00
2026-11-15T00:00:00+01:00
```

A day in `to` covers that whole day, so the range ends at the start of the next day.

### Query errors

```go
type eventsList struct {
	apicontract.Page
	apicontract.Range
	PageSize int `query:"page_size" deprecated:"limit"`
}
```

`GET /events?limit=ten`, then `GET /events?from=2026-11-15&to=2026-11-14&colour=red`:

```text
400 application/problem+json
{
  "type": "https://github.com/whilesmartgo/apicontract/blob/main/docs/errors.md#malformed_query",
  "title": "The query string could not be parsed.",
  "status": 400,
  "code": "malformed_query",
  "errors": [
    {
      "in": "query",
      "name": "limit",
      "code": "invalid",
      "detail": "must be an integer"
    }
  ]
}
422 application/problem+json
{
  "type": "https://github.com/whilesmartgo/apicontract/blob/main/docs/errors.md#invalid_query",
  "title": "The query string is not valid.",
  "status": 422,
  "code": "invalid_query",
  "errors": [
    {
      "in": "query",
      "name": "colour",
      "code": "unknown_field",
      "detail": "is not a parameter of this route"
    },
    {
      "in": "query",
      "name": "to",
      "code": "out_of_range",
      "detail": "must be after from"
    }
  ]
}
```

A declared value that cannot be parsed makes the response a `400` with the collected field errors. Otherwise rule violations, including undeclared parameters, return `422`.

### Keep an old parameter name working

`page_size` above is the old name for `limit`. `GET /events?page_size=20`, then `GET /events?page_size=20&limit=30`:

```text
200 application/json
{
  "limit": 20
}
422 application/problem+json
{
  "type": "https://github.com/whilesmartgo/apicontract/blob/main/docs/errors.md#invalid_query",
  "title": "The query string is not valid.",
  "status": 422,
  "code": "invalid_query",
  "errors": [
    {
      "in": "query",
      "name": "page_size",
      "code": "conflict",
      "detail": "page_size and limit disagree; send only limit"
    }
  ]
}
```

### Read a JSON body

```go
type newEvent struct {
	Title     string    `json:"title" required:"true" maxLength:"200"`
	StartsAt  time.Time `json:"starts_at" required:"true"`
	Capacity  *int      `json:"capacity" minimum:"1"`
	Attendees []struct {
		Email string `json:"email" required:"true"`
	} `json:"attendees" maxItems:"50"`
}

func createEvent(w http.ResponseWriter, r *http.Request) {
	var body newEvent
	if err := apicontract.BindJSON(r, &body); err != nil {
		apicontract.WriteProblem(w, err)
		return
	}
	// save the event
	apicontract.WriteJSON(w, http.StatusCreated, body)
}
```

Three `POST /events` requests:

1. `{"title": "Planning", "starts_at": "2026-11-14T09:00:00+01:00", "attendees": [{"email": "ada@example.com"}]}`
2. `{"starts_at": "2026-11-14T09:00:00", "capacity": 0, "attendees": [{"name": "Ada"}]}`
3. `{"capacity": 0, "attendees": [{"name": "Ada"}]}`

```text
201 application/json
{
  "title": "Planning",
  "starts_at": "2026-11-14T09:00:00+01:00",
  "capacity": null,
  "attendees": [
    {
      "email": "ada@example.com"
    }
  ]
}
400 application/problem+json
{
  "type": "https://github.com/whilesmartgo/apicontract/blob/main/docs/errors.md#malformed_body",
  "title": "The request body could not be parsed.",
  "status": 400,
  "code": "malformed_body",
  "errors": [
    {
      "in": "body",
      "name": "title",
      "code": "required",
      "detail": "is required"
    },
    {
      "in": "body",
      "name": "starts_at",
      "code": "invalid",
      "detail": "must be an RFC 3339 timestamp with offset, such as 2026-11-14T09:00:00+01:00"
    },
    {
      "in": "body",
      "name": "capacity",
      "code": "out_of_range",
      "detail": "must be at least 1"
    },
    {
      "in": "body",
      "name": "attendees[0].name",
      "code": "unknown_field",
      "detail": "is not a field of this request"
    },
    {
      "in": "body",
      "name": "attendees[0].email",
      "code": "required",
      "detail": "is required"
    }
  ]
}
422 application/problem+json
{
  "type": "https://github.com/whilesmartgo/apicontract/blob/main/docs/errors.md#invalid_body",
  "title": "The request body is not valid.",
  "status": 422,
  "code": "invalid_body",
  "errors": [
    {
      "in": "body",
      "name": "title",
      "code": "required",
      "detail": "is required"
    },
    {
      "in": "body",
      "name": "starts_at",
      "code": "required",
      "detail": "is required"
    },
    {
      "in": "body",
      "name": "capacity",
      "code": "out_of_range",
      "detail": "must be at least 1"
    },
    {
      "in": "body",
      "name": "attendees[0].name",
      "code": "unknown_field",
      "detail": "is not a field of this request"
    },
    {
      "in": "body",
      "name": "attendees[0].email",
      "code": "required",
      "detail": "is required"
    }
  ]
}
```

The second request has a timestamp without an offset, which cannot be parsed, so it is a `400`. The third parses, so the same rule breaks are a `422`. Nested fields are named by their path, such as `attendees[0].email`. Optional pointers such as `capacity`, maps and free-form fields accept `null`, which clears the field; other fields reject it. Integer fields accept whole JSON numbers such as `100`, `100.0` or `1e2`, within signed 64-bit and parser exponent limits. Values must also fit the Go field type.

### Bodies the parser refuses before reading fields

A form-encoded body, then a JSON object that repeats a key:

```text
415 application/problem+json
{
  "type": "https://github.com/whilesmartgo/apicontract/blob/main/docs/errors.md#unsupported_media_type",
  "title": "The request body must be JSON.",
  "status": 415,
  "detail": "Send the body as application/json.",
  "code": "unsupported_media_type"
}
400 application/problem+json
{
  "type": "https://github.com/whilesmartgo/apicontract/blob/main/docs/errors.md#malformed_body",
  "title": "The request body could not be parsed.",
  "status": 400,
  "detail": "The request body is not valid JSON: duplicate key \"title\".",
  "code": "malformed_body"
}
```

The HTTP parser also accepts application subtypes ending in +json. A declared charset must be UTF-8. Bodies over 1 MiB get `413` with code `body_too_large`. Pass `apicontract.MaxBytes(n)` to change the limit. `BindJSONBytes` reads a JSON object you already hold, such as an MCP tool call's arguments, with the same limit and rules.

### Check an idempotency key

```go
key, err := apicontract.IdempotencyKey(r) // "" when the header is absent
```

With `Idempotency-Key: short`, `err` is written as:

```text
{"type":"https://github.com/whilesmartgo/apicontract/blob/main/docs/errors.md#malformed_header","title":"A request header could not be parsed.","status":400,"code":"malformed_header","errors":[{"in":"header","name":"Idempotency-Key","code":"invalid","detail":"must be 8 to 128 characters"}]}
```

`"8e03978e-40d5-43e8-bc93-6894a57f9324"`, with the quotes, and `8e03978e-40d5-43e8-bc93-6894a57f9324` both return the key without quotes. Two header fields or a comma-joined pair of keys are rejected. Commas inside a quoted key are allowed. Storing keys and replaying the first response is up to the app.

### Describe a body for tools and specs

`BodySchema` describes what clients should send to `BindJSON` for a body struct. Use it for MCP tool input schemas. It includes the ranges of `int8`, `int16` and `int32` fields. It omits the full range of 64-bit fields because JavaScript numbers cannot represent those limits exactly. The parser still rejects values outside that range. Integer bounds round inward: `maximum:"9.5"` means 9. With `clamp:"true"`, the schema keeps `maximum` as the limit clients should use. The parser also accepts larger values and caps them.

```go
schema, _ := apicontract.BodySchema(newEvent{})
```

```text
{
  "additionalProperties": false,
  "properties": {
    "attendees": {
      "items": {
        "additionalProperties": false,
        "properties": {
          "email": {
            "type": "string"
          }
        },
        "required": [
          "email"
        ],
        "type": "object"
      },
      "maxItems": 50,
      "type": "array"
    },
    "capacity": {
      "minimum": 1,
      "type": [
        "integer",
        "null"
      ]
    },
    "starts_at": {
      "format": "date-time",
      "type": "string"
    },
    "title": {
      "maxLength": 200,
      "type": "string"
    }
  },
  "required": [
    "title",
    "starts_at"
  ],
  "type": "object"
}
```

Tags the parsers read:

| Tag | Applies to | Meaning |
| --- | --- | --- |
| `query:"name"` | query | Parameter name. On a nested struct, a prefix: `query:"updated"` reads `updated_from` and `updated_to`. |
| `json:"name"` | body | Field name using `encoding/json` tag syntax. An omitted name uses the Go field name. |
| `required:"true"` | both | Must be present and not null. Query values must be nonblank. An old name with a value can satisfy its replacement; JSON Schemas express this alternative. |
| `default:"value"` | both | Used for absent optional fields. Blank query values count as absent. Body fields take defaults for strings, integers, numbers and booleans. A default must meet the field's rules, stay within its maximum even when it clamps, and fit its Go type, or binding fails with a programming error. |
| `minimum`, `maximum` | integers, numbers | Bounds. Add `clamp:"true"` to cap values above the maximum; values below the minimum are rejected. |
| `maxLength`, `enum` on a list | each entry of a string list; named types such as `type Tags []string` are supported in bodies | Maximum characters and allowed values per entry. |
| `maxItems` | lists, sort | Maximum items. |
| `enum:"a,b"` | strings, string lists, sort | Allowed values. |
| `deprecated:"new_name"` | both | An old name that fills `new_name` when that is absent. Both with different values is a conflict. |
| `doc` | both | Description in schemas. |
| `bound:"end"` | query times | A day means the end of that day. |

Supported body types: strings, integers, floats, booleans, `time.Time`, `apicontract.Date`, structs, slices of these, pointers to these, `map[string]any` for any JSON object, and `any` or `json.RawMessage` for any JSON value. Free-form values keep numbers as `json.Number`, so large integers are not rounded.

Other describers:

- `InputSchema(query)` returns the JSON Schema of a query struct.
- `OpenAPIParameters(query)` and `OpenAPIRequestBody(body)` return OpenAPI 3.1 objects for specs built in code.
- `OpenAPIProblemResponse(status)` returns an OpenAPI response with the problem body.
- `OpenAPIParametersOf` and `OpenAPIRequestBodyOf` take `[]Field` built from source types, for generators that read Go code.

## Check an OpenAPI document

The `apicontract` command checks OpenAPI 3.0 and 3.1 JSON documents from any language. It checks GET, PUT, POST, PATCH, DELETE, HEAD, OPTIONS and TRACE operations and follows local references. Its schema checks read keywords beside a `$ref` and members of `allOf` together with the referenced schema. A document whose schemas are too large or too deeply composed to walk is reported as an error rather than partly checked. It does not resolve external references or validate the full OpenAPI specification.

```sh
go run github.com/whilesmartgo/apicontract/cmd/apicontract@latest lint openapi.json
```

On a document with a few common problems:

```text
openapi.json: GET /notes: error-media-type: the 400 response must be application/problem+json
openapi.json: GET /notes: list-cursor: collections must accept an opaque cursor
openapi.json: GET /notes: list-envelope: collections must return {"items": [...], "next_cursor": "..."}
openapi.json: GET /notes: list-limit: collections must accept limit with maximum 200
openapi.json: GET /notes: param-legacy: query parameter "page" must be deprecated in favour of "cursor"
openapi.json: GET /notes: param-name: query parameter "updatedSince" must be snake_case
openapi.json: POST /notes: body-closed: request objects must set additionalProperties to false so unknown fields are rejected
openapi.json: POST /notes: body-time-format: body field "remind_at" must declare format date-time
1 document, 8 violations, 0 accepted by exceptions, 0 stale exceptions
```

| Rule | Fails when |
| --- | --- |
| `contract-version` | `info.x-whilesmart-contract` is missing, empty or not a string. |
| `param-name` | A query parameter is not snake_case. |
| `param-legacy` | An old name such as `page_size` is not marked deprecated. |
| `param-time-format` | A parameter named `from`, `to`, `since` or `until`, or ending in `_from`, `_to`, `_since` or `_until`, has no `date` or `date-time` format, directly or in `anyOf` or `oneOf`. |
| `response-schema` | A GET route declares a 200 response with no content, or with `application/json` but no nonempty schema. |
| `list-envelope` | A collection's 200 response schema has no required `items` property typed only as an array, directly or through `allOf`. |
| `list-limit` | A collection has no `limit`, or `limit` is not an integer with a declared maximum of at most 200. |
| `list-cursor` | A collection has no `cursor`. |
| `error-responses` | An operation with query parameters or a body declares none of 400, 422 or `4XX`. |
| `error-media-type` | A declared 4xx or 5xx response, including `4XX` and `5XX`, resolves to an object with no `application/problem+json` content. |
| `body-name` | A JSON request body field, at any depth, is not snake_case. |
| `body-time-format` | A JSON request body field ending in `_at` has no `date-time` format. |
| `body-closed` | A JSON request body schema of type `object` with properties does not set `additionalProperties: false`. |

A GET route is a collection when its 200 `application/json` schema, a schema it combines through `allOf`, or one of its `anyOf` or `oneOf` options is an array, or when it has an array in `items` or `data`, or when the route declares `limit`, `cursor`, `page`, `per_page`, `page_size` or `offset`.

### Adopt it on an existing API

Record current violations with a reason, then run the check in CI:

```sh
apicontract lint -exceptions api-contract.exceptions.json -seed "predates the contract" openapi.json
apicontract lint -exceptions api-contract.exceptions.json openapi.json
```

Violations that match no exception fail the run. An exception that matches no violation in a checked document also fails. An entry may use `*` for `rule`, `method` or `path`, and may omit `subject` to cover every parameter or field. Broad exceptions can accept new violations. Running `-seed` again adds exceptions for current violations and removes stale ones in checked documents. `-report file.md` writes accepted violations as a Markdown table.

## License

MIT
