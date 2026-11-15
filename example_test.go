package apicontract_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/whilesmartgo/apicontract"
)

func serve(h http.Handler, method, target, body string) {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	show(recorder)
}

func show(recorder *httptest.ResponseRecorder) {
	var indented bytes.Buffer
	body := bytes.TrimSpace(recorder.Body.Bytes())
	if err := json.Indent(&indented, body, "", "  "); err != nil {
		indented.Write(body)
	}
	fmt.Printf("%d %s\n%s\n", recorder.Code, recorder.Header().Get("Content-Type"), indented.String())
}

type note struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type notesList struct {
	apicontract.Page
	apicontract.Search
}

func listNotes(cursors *apicontract.Cursors, all []note) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var q notesList
		if err := apicontract.BindQuery(r, &q); err != nil {
			_ = apicontract.WriteProblem(w, err)
			return
		}
		binding := apicontract.RequestBinding(r, "user-1")
		var after struct {
			ID int `json:"id"`
		}
		if q.Cursor != "" {
			if err := cursors.Decode(q.Cursor, binding, &after); err != nil {
				_ = apicontract.WriteProblem(w, err)
				return
			}
		}
		start := after.ID
		end := min(start+q.Limit, len(all))
		next := ""
		if end < len(all) {
			next, _ = cursors.Encode(binding, map[string]int{"id": end})
		}
		_ = apicontract.WriteList(w, all[start:end], next)
	}
}

func Example_paging() {
	cursors, _ := apicontract.NewCursors([]byte("an example key of at least 32 bytes"))
	h := listNotes(cursors, []note{{1, "Budget"}, {2, "Roadmap"}, {3, "Hiring"}})

	serve(h, "GET", "/notes?limit=2", "")
	serve(h, "GET", "/notes?limit=2&cursor=eyJpZCI6Mn0.cuGm7m76S3bOizAsEbyVpHEsw-5bafeEGFHJ26VFq-E", "")
	serve(h, "GET", "/notes?limit=2&q=budget&cursor=eyJpZCI6Mn0.cuGm7m76S3bOizAsEbyVpHEsw-5bafeEGFHJ26VFq-E", "")
	// Output:
	// 200 application/json
	// {
	//   "items": [
	//     {
	//       "id": 1,
	//       "title": "Budget"
	//     },
	//     {
	//       "id": 2,
	//       "title": "Roadmap"
	//     }
	//   ],
	//   "next_cursor": "eyJpZCI6Mn0.cuGm7m76S3bOizAsEbyVpHEsw-5bafeEGFHJ26VFq-E"
	// }
	// 200 application/json
	// {
	//   "items": [
	//     {
	//       "id": 3,
	//       "title": "Hiring"
	//     }
	//   ]
	// }
	// 400 application/problem+json
	// {
	//   "type": "https://github.com/whilesmartgo/apicontract/blob/main/docs/errors.md#malformed_query",
	//   "title": "The query string could not be parsed.",
	//   "status": 400,
	//   "code": "malformed_query",
	//   "errors": [
	//     {
	//       "in": "query",
	//       "name": "cursor",
	//       "code": "invalid_cursor",
	//       "detail": "is not a cursor issued for this query"
	//     }
	//   ]
	// }
}

func Example_dayRange() {
	var q struct {
		apicontract.Range
		apicontract.Zone
	}
	request := httptest.NewRequest("GET", "/events?from=2026-11-14&to=2026-11-14&tz=Europe/Berlin", nil)
	if err := apicontract.BindQuery(request, &q); err != nil {
		panic(err)
	}
	fmt.Println(q.From.Format(time.RFC3339))
	fmt.Println(q.To.Format(time.RFC3339))
	// Output:
	// 2026-11-14T00:00:00+01:00
	// 2026-11-15T00:00:00+01:00
}

type eventsList struct {
	apicontract.Page
	apicontract.Range
	PageSize int `query:"page_size" deprecated:"limit"`
}

func listEvents(w http.ResponseWriter, r *http.Request) {
	var q eventsList
	if err := apicontract.BindQuery(r, &q); err != nil {
		_ = apicontract.WriteProblem(w, err)
		return
	}
	_ = apicontract.WriteJSON(w, http.StatusOK, map[string]any{"limit": q.Limit})
}

func Example_queryErrors() {
	h := http.HandlerFunc(listEvents)
	serve(h, "GET", "/events?limit=ten", "")
	serve(h, "GET", "/events?from=2026-11-15&to=2026-11-14&colour=red", "")
	// Output:
	// 400 application/problem+json
	// {
	//   "type": "https://github.com/whilesmartgo/apicontract/blob/main/docs/errors.md#malformed_query",
	//   "title": "The query string could not be parsed.",
	//   "status": 400,
	//   "code": "malformed_query",
	//   "errors": [
	//     {
	//       "in": "query",
	//       "name": "limit",
	//       "code": "invalid",
	//       "detail": "must be an integer"
	//     }
	//   ]
	// }
	// 422 application/problem+json
	// {
	//   "type": "https://github.com/whilesmartgo/apicontract/blob/main/docs/errors.md#invalid_query",
	//   "title": "The query string is not valid.",
	//   "status": 422,
	//   "code": "invalid_query",
	//   "errors": [
	//     {
	//       "in": "query",
	//       "name": "colour",
	//       "code": "unknown_field",
	//       "detail": "is not a parameter of this route"
	//     },
	//     {
	//       "in": "query",
	//       "name": "to",
	//       "code": "out_of_range",
	//       "detail": "must be after from"
	//     }
	//   ]
	// }
}

func Example_renamedParameter() {
	h := http.HandlerFunc(listEvents)
	serve(h, "GET", "/events?page_size=20", "")
	serve(h, "GET", "/events?page_size=20&limit=30", "")
	// Output:
	// 200 application/json
	// {
	//   "limit": 20
	// }
	// 422 application/problem+json
	// {
	//   "type": "https://github.com/whilesmartgo/apicontract/blob/main/docs/errors.md#invalid_query",
	//   "title": "The query string is not valid.",
	//   "status": 422,
	//   "code": "invalid_query",
	//   "errors": [
	//     {
	//       "in": "query",
	//       "name": "page_size",
	//       "code": "conflict",
	//       "detail": "page_size and limit disagree; send only limit"
	//     }
	//   ]
	// }
}

type newEvent struct {
	Title     string    `json:"title" required:"true" maxLength:"200"`
	StartsAt  time.Time `json:"starts_at" required:"true"`
	Capacity  *int      `json:"capacity" minimum:"1"`
	Attendees []struct {
		Email string `json:"email" required:"true"`
	} `json:"attendees" maxItems:"50"`
}

func createEventHandler(w http.ResponseWriter, r *http.Request) {
	var body newEvent
	if err := apicontract.BindJSON(r, &body); err != nil {
		_ = apicontract.WriteProblem(w, err)
		return
	}
	_ = apicontract.WriteJSON(w, http.StatusCreated, body)
}

func Example_requestBody() {
	h := http.HandlerFunc(createEventHandler)
	serve(h, "POST", "/events", `{"title": "Planning", "starts_at": "2026-11-14T09:00:00+01:00", "attendees": [{"email": "ada@example.com"}]}`)
	serve(h, "POST", "/events", `{"starts_at": "2026-11-14T09:00:00", "capacity": 0, "attendees": [{"name": "Ada"}]}`)
	serve(h, "POST", "/events", `{"capacity": 0, "attendees": [{"name": "Ada"}]}`)
	// Output:
	// 201 application/json
	// {
	//   "title": "Planning",
	//   "starts_at": "2026-11-14T09:00:00+01:00",
	//   "capacity": null,
	//   "attendees": [
	//     {
	//       "email": "ada@example.com"
	//     }
	//   ]
	// }
	// 400 application/problem+json
	// {
	//   "type": "https://github.com/whilesmartgo/apicontract/blob/main/docs/errors.md#malformed_body",
	//   "title": "The request body could not be parsed.",
	//   "status": 400,
	//   "code": "malformed_body",
	//   "errors": [
	//     {
	//       "in": "body",
	//       "name": "title",
	//       "code": "required",
	//       "detail": "is required"
	//     },
	//     {
	//       "in": "body",
	//       "name": "starts_at",
	//       "code": "invalid",
	//       "detail": "must be an RFC 3339 timestamp with offset, such as 2026-11-14T09:00:00+01:00"
	//     },
	//     {
	//       "in": "body",
	//       "name": "capacity",
	//       "code": "out_of_range",
	//       "detail": "must be at least 1"
	//     },
	//     {
	//       "in": "body",
	//       "name": "attendees[0].name",
	//       "code": "unknown_field",
	//       "detail": "is not a field of this request"
	//     },
	//     {
	//       "in": "body",
	//       "name": "attendees[0].email",
	//       "code": "required",
	//       "detail": "is required"
	//     }
	//   ]
	// }
	// 422 application/problem+json
	// {
	//   "type": "https://github.com/whilesmartgo/apicontract/blob/main/docs/errors.md#invalid_body",
	//   "title": "The request body is not valid.",
	//   "status": 422,
	//   "code": "invalid_body",
	//   "errors": [
	//     {
	//       "in": "body",
	//       "name": "title",
	//       "code": "required",
	//       "detail": "is required"
	//     },
	//     {
	//       "in": "body",
	//       "name": "starts_at",
	//       "code": "required",
	//       "detail": "is required"
	//     },
	//     {
	//       "in": "body",
	//       "name": "capacity",
	//       "code": "out_of_range",
	//       "detail": "must be at least 1"
	//     },
	//     {
	//       "in": "body",
	//       "name": "attendees[0].name",
	//       "code": "unknown_field",
	//       "detail": "is not a field of this request"
	//     },
	//     {
	//       "in": "body",
	//       "name": "attendees[0].email",
	//       "code": "required",
	//       "detail": "is required"
	//     }
	//   ]
	// }
}

func Example_requestBodyRejected() {
	h := http.HandlerFunc(createEventHandler)
	request := httptest.NewRequest("POST", "/events", strings.NewReader("title=Planning"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	show(recorder)

	serve(h, "POST", "/events", `{"title": "a", "title": "b"}`)
	// Output:
	// 415 application/problem+json
	// {
	//   "type": "https://github.com/whilesmartgo/apicontract/blob/main/docs/errors.md#unsupported_media_type",
	//   "title": "The request body must be JSON.",
	//   "status": 415,
	//   "detail": "Send the body as application/json.",
	//   "code": "unsupported_media_type"
	// }
	// 400 application/problem+json
	// {
	//   "type": "https://github.com/whilesmartgo/apicontract/blob/main/docs/errors.md#malformed_body",
	//   "title": "The request body could not be parsed.",
	//   "status": 400,
	//   "detail": "The request body is not valid JSON: duplicate key \"title\".",
	//   "code": "malformed_body"
	// }
}

func ExampleIdempotencyKey() {
	request := httptest.NewRequest("POST", "/events", nil)
	request.Header.Set("Idempotency-Key", "short")
	_, err := apicontract.IdempotencyKey(request)
	data, _ := json.Marshal(err)
	fmt.Println(string(data))
	// Output:
	// {"type":"https://github.com/whilesmartgo/apicontract/blob/main/docs/errors.md#malformed_header","title":"A request header could not be parsed.","status":400,"code":"malformed_header","errors":[{"in":"header","name":"Idempotency-Key","code":"invalid","detail":"must be 8 to 128 characters"}]}
}

func ExampleBodySchema() {
	schema, _ := apicontract.BodySchema(newEvent{})
	data, _ := json.MarshalIndent(schema, "", "  ")
	fmt.Println(string(data))
	// Output:
	// {
	//   "additionalProperties": false,
	//   "properties": {
	//     "attendees": {
	//       "items": {
	//         "additionalProperties": false,
	//         "properties": {
	//           "email": {
	//             "type": "string"
	//           }
	//         },
	//         "required": [
	//           "email"
	//         ],
	//         "type": "object"
	//       },
	//       "maxItems": 50,
	//       "type": "array"
	//     },
	//     "capacity": {
	//       "minimum": 1,
	//       "type": [
	//         "integer",
	//         "null"
	//       ]
	//     },
	//     "starts_at": {
	//       "format": "date-time",
	//       "type": "string"
	//     },
	//     "title": {
	//       "maxLength": 200,
	//       "type": "string"
	//     }
	//   },
	//   "required": [
	//     "title",
	//     "starts_at"
	//   ],
	//   "type": "object"
	// }
}
