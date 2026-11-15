package apicontract_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/whilesmartgo/apicontract"
)

type attendee struct {
	Email string `json:"email" required:"true" maxLength:"254"`
	Role  string `json:"role" enum:"required,optional" default:"required"`
}

type createEvent struct {
	Title     string           `json:"title" required:"true" maxLength:"200"`
	StartsAt  time.Time        `json:"starts_at" required:"true"`
	Day       apicontract.Date `json:"day"`
	Capacity  *int             `json:"capacity" minimum:"1" maximum:"500"`
	Price     float64          `json:"price" minimum:"0"`
	Tags      []string         `json:"tags" maxItems:"3" enum:"work,home"`
	Attendees []attendee       `json:"attendees" maxItems:"50"`
	Location  *struct {
		Name string `json:"name" required:"true"`
	} `json:"location"`
	Metadata map[string]any `json:"metadata"`
	Name     string         `json:"name" deprecated:"title"`
}

func createHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body createEvent
		if err := apicontract.BindJSON(r, &body, apicontract.MaxBytes(2048)); err != nil {
			_ = apicontract.WriteProblem(w, err)
			return
		}
		_ = apicontract.WriteJSON(w, http.StatusCreated, body)
	})
}

func post(t *testing.T, contentType, body string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	createHandler().ServeHTTP(recorder, request)
	var decoded map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("%s: %v", recorder.Body.String(), err)
	}
	return recorder, decoded
}

func errorEntries(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	list, _ := body["errors"].([]any)
	entries := make([]map[string]any, len(list))
	for i, entry := range list {
		entries[i] = entry.(map[string]any)
	}
	return entries
}

func TestBodyBindsEveryFieldType(t *testing.T) {
	recorder, body := post(t, "application/json", `{
		"title": "Planning",
		"starts_at": "2026-11-14T09:00:00+01:00",
		"day": "2026-11-14",
		"capacity": 20,
		"price": 12.5,
		"tags": ["work"],
		"attendees": [{"email": "a@example.com"}],
		"location": {"name": "Room 1"},
		"metadata": {"source": "import", "rank": 2}
	}`)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status %d: %v", recorder.Code, body)
	}
	attendees := body["attendees"].([]any)
	if attendees[0].(map[string]any)["role"] != "required" {
		t.Fatalf("element defaults must apply: %v", attendees)
	}
	if body["day"] != "2026-11-14" || body["capacity"].(float64) != 20 || body["price"].(float64) != 12.5 {
		t.Fatalf("got %v", body)
	}
}

func TestBodyStatusCodes(t *testing.T) {
	cases := []struct {
		name, contentType, body string
		status                  int
		code                    string
		errors                  []string
	}{
		{"no content type", "", `{}`, 415, apicontract.CodeUnsupportedMediaType, nil},
		{"form content type", "application/x-www-form-urlencoded", `title=x`, 415, apicontract.CodeUnsupportedMediaType, nil},
		{"latin1", "application/json; charset=latin1", `{}`, 415, apicontract.CodeUnsupportedMediaType, nil},
		{"too large", "application/json", `{"title": "` + strings.Repeat("x", 3000) + `"}`, 413, apicontract.CodeBodyTooLarge, nil},
		{"empty", "application/json", ``, 400, apicontract.CodeMalformedBody, nil},
		{"not json", "application/json", `{title: 1}`, 400, apicontract.CodeMalformedBody, nil},
		{"array", "application/json", `[]`, 400, apicontract.CodeMalformedBody, nil},
		{"trailing data", "application/json", `{} {}`, 400, apicontract.CodeMalformedBody, nil},
		{"duplicate key", "application/json", `{"title": "a", "title": "b"}`, 400, apicontract.CodeMalformedBody, nil},
		{"too deep", "application/json", `{"metadata": ` + strings.Repeat(`{"a":`, 70) + `1` + strings.Repeat(`}`, 70) + `}`, 400, apicontract.CodeMalformedBody, nil},
		{"missing", "application/json", `{}`, 422, apicontract.CodeInvalidBody, []string{"title:required", "starts_at:required"}},
		{"null required", "application/json", `{"title": null, "starts_at": "2026-11-14T09:00:00Z"}`, 422, apicontract.CodeInvalidBody, []string{"title:required"}},
		{"unknown", "application/json", `{"title": "a", "starts_at": "2026-11-14T09:00:00Z", "colour": "red", "location": {"name": "x", "floor": 2}}`, 422, apicontract.CodeInvalidBody, []string{"colour:unknown_field", "location.floor:unknown_field"}},
		{"rules", "application/json", `{"title": "a", "starts_at": "2026-11-14T09:00:00Z", "capacity": 0, "price": -1, "tags": ["work", "play"], "attendees": [{"email": "a@example.com", "role": "host"}]}`, 422, apicontract.CodeInvalidBody, []string{"capacity:out_of_range", "price:out_of_range", "tags[1]:unknown_value", "attendees[0].role:unknown_value"}},
		{"too many", "application/json", `{"title": "a", "starts_at": "2026-11-14T09:00:00Z", "tags": ["work", "work", "work", "work"]}`, 422, apicontract.CodeInvalidBody, []string{"tags:too_many"}},
		{"wrong types", "application/json", `{"title": 5, "starts_at": "2026-11-14T09:00:00", "capacity": 1.5, "attendees": [{"email": "a"}, null]}`, 400, apicontract.CodeMalformedBody, []string{"title:invalid", "starts_at:invalid", "capacity:invalid", "attendees[1]:invalid"}},
		{"null for non-pointer", "application/json", `{"title": "a", "starts_at": "2026-11-14T09:00:00Z", "price": null}`, 400, apicontract.CodeMalformedBody, []string{"price:invalid"}},
		{"alias conflict", "application/json", `{"title": "a", "name": "b", "starts_at": "2026-11-14T09:00:00Z"}`, 422, apicontract.CodeInvalidBody, []string{"name:conflict"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			recorder, body := post(t, c.contentType, c.body)
			if recorder.Code != c.status || body["code"] != c.code || int(body["status"].(float64)) != c.status {
				t.Fatalf("status %d body %v", recorder.Code, body)
			}
			if recorder.Header().Get("Content-Type") != apicontract.ProblemContentType {
				t.Fatalf("content type %q", recorder.Header().Get("Content-Type"))
			}
			if c.errors == nil {
				return
			}
			var got []string
			for _, entry := range errorEntries(t, body) {
				if entry["in"] != apicontract.InBody {
					t.Fatalf("entry %v", entry)
				}
				got = append(got, entry["name"].(string)+":"+entry["code"].(string))
			}
			if strings.Join(got, " ") != strings.Join(c.errors, " ") {
				t.Fatalf("errors %v, want %v", got, c.errors)
			}
		})
	}
}

func TestBodyAliasFillsNewName(t *testing.T) {
	recorder, body := post(t, "application/json", `{"name": "Old client", "starts_at": "2026-11-14T09:00:00Z", "capacity": null}`)
	if recorder.Code != http.StatusCreated || body["title"] != "Old client" || body["capacity"] != nil {
		t.Fatalf("status %d body %v", recorder.Code, body)
	}
}

func TestBodyPlanErrors(t *testing.T) {
	type dangling struct {
		Old string `json:"old" deprecated:"new"`
	}
	type badDefault struct {
		Count int `json:"count" default:"many"`
	}
	type channel struct {
		C chan int `json:"c"`
	}
	type node struct {
		Children []node `json:"children"`
	}
	for _, target := range []any{&dangling{}, &badDefault{}, &channel{}, &node{}} {
		if err := apicontract.BindJSONBytes([]byte(`{}`), target); err == nil || strings.Contains(err.Error(), "request") {
			t.Fatalf("%T: expected a programming error, got %v", target, err)
		}
	}
}

func TestBodySchemaMatchesBinder(t *testing.T) {
	schema, err := apicontract.BodySchema(createEvent{})
	if err != nil {
		t.Fatal(err)
	}
	properties := schema["properties"].(map[string]any)
	checks := map[string]string{
		"starts_at": `{"format":"date-time","type":"string"}`,
		"day":       `{"format":"date","type":"string"}`,
		"capacity":  `{"maximum":500,"minimum":1,"type":["integer","null"]}`,
		"tags":      `{"items":{"enum":["work","home"],"type":"string"},"maxItems":3,"type":"array"}`,
		"location":  `{"additionalProperties":false,"properties":{"name":{"type":"string"}},"required":["name"],"type":["object","null"]}`,
		"name":      `{"deprecated":true,"description":"Deprecated: use title.","type":"string"}`,
	}
	for name, want := range checks {
		got, _ := json.Marshal(properties[name])
		if string(got) != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
	if schema["additionalProperties"] != false || strings.Join(schema["required"].([]string), ",") != "starts_at" {
		t.Fatalf("got %v", schema)
	}
	alternatives, _ := json.Marshal(schema["allOf"])
	if string(alternatives) != `[{"anyOf":[{"properties":{"title":{"not":{"type":"null"}}},"required":["title"]},{"properties":{"name":{"not":{"type":"null"}}},"required":["name"]}]}]` {
		t.Fatalf("title may be sent as its old name name: %s", alternatives)
	}
}

func TestIdempotencyKey(t *testing.T) {
	cases := map[string]int{"": 0, "abcdefgh": 0, `"8e03978e-40d5-43e8-bc93-6894a57f9324"`: 0, "short": 400, "has space in it": 400, strings.Repeat("k", 129): 400}
	for header, status := range cases {
		request := httptest.NewRequest(http.MethodPost, "/", nil)
		if header != "" {
			request.Header.Set(apicontract.IdempotencyHeader, header)
		}
		_, err := apicontract.IdempotencyKey(request)
		if status == 0 && err != nil || status != 0 && (err == nil || err.(*apicontract.Problem).Status != status) {
			t.Fatalf("%q: %v", header, err)
		}
	}
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	request.Header.Add(apicontract.IdempotencyHeader, "abcdefgh")
	request.Header.Add(apicontract.IdempotencyHeader, "abcdefgi")
	if _, err := apicontract.IdempotencyKey(request); err == nil {
		t.Fatal("a repeated key must be rejected")
	}
}
