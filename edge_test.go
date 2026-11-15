package apicontract_test

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/whilesmartgo/apicontract"
)

func TestZeroCursorsRefuseToSign(t *testing.T) {
	var cursors apicontract.Cursors
	if _, err := cursors.Encode("b", 1); err == nil {
		t.Fatal("a Cursors without a key must not sign")
	}
	err := cursors.Decode("e30.c2ln", "b", &struct{}{})
	var problem *apicontract.Problem
	if err == nil || errors.As(err, &problem) {
		t.Fatalf("a Cursors without a key must fail as a programming error, got %v", err)
	}
}

func TestRequestBindingIncludesTheRoute(t *testing.T) {
	a := httptest.NewRequest("GET", "/projects/a/events?limit=2", nil)
	b := httptest.NewRequest("GET", "/projects/b/events?limit=5", nil)
	c := httptest.NewRequest("GET", "/projects/a/events?limit=9", nil)
	if apicontract.RequestBinding(a, "u1") == apicontract.RequestBinding(b, "u1") {
		t.Fatal("cursors from one resource must not verify on another")
	}
	if apicontract.RequestBinding(a, "u1") != apicontract.RequestBinding(c, "u1") {
		t.Fatal("changing limit must keep the binding")
	}
}

func TestBodyNullClearsAndNumbersStayExact(t *testing.T) {
	type patch struct {
		Capacity *int           `json:"capacity"`
		Count    int            `json:"count"`
		Metadata map[string]any `json:"metadata"`
		Raw      any            `json:"raw"`
	}
	start := 20
	target := patch{Capacity: &start, Raw: "old"}
	err := apicontract.BindJSONBytes([]byte(`{"capacity": null, "raw": null, "count": 1e2, "metadata": {"id": 9007199254740993}}`), &target)
	if err != nil {
		t.Fatal(err)
	}
	if target.Capacity != nil || target.Raw != nil {
		t.Fatalf("null must clear: %+v", target)
	}
	if target.Count != 100 {
		t.Fatalf("1e2 is an integer: %d", target.Count)
	}
	if id := target.Metadata["id"].(json.Number).String(); id != "9007199254740993" {
		t.Fatalf("free-form numbers must not be rounded: %s", id)
	}
	err = apicontract.BindJSONBytes([]byte(`{"metadata": 42}`), &target)
	if p := problemOf(t, err); p.Status != http.StatusBadRequest {
		t.Fatalf("a map field takes only objects: %+v", p)
	}
	err = apicontract.BindJSONBytes([]byte(`{"count": 1.5}`), &target)
	if p := problemOf(t, err); p.Status != http.StatusBadRequest {
		t.Fatalf("got %+v", p)
	}
}

func TestDeclaredDefaultsAreChecked(t *testing.T) {
	type overflow struct {
		N int8 `json:"n" default:"128"`
	}
	type belowMinimum struct {
		N int `json:"n" default:"0" minimum:"1"`
	}
	type notInEnum struct {
		S string `json:"s" default:"x" enum:"a,b"`
	}
	type notANumber struct {
		F float64 `json:"f" default:"NaN"`
	}
	type badQueryDefault struct {
		On bool `query:"on" default:"maybe"`
	}
	for _, target := range []any{&overflow{}, &belowMinimum{}, &notInEnum{}, &notANumber{}} {
		if err := apicontract.BindJSONBytes([]byte(`{}`), target); err == nil || errors.As(err, new(*apicontract.Problem)) {
			t.Fatalf("%T: expected a programming error, got %v", target, err)
		}
	}
	if err := apicontract.BindValues(url.Values{}, &badQueryDefault{}); err == nil || errors.As(err, new(*apicontract.Problem)) {
		t.Fatalf("expected a programming error, got %v", err)
	}
}

func TestLargeIntegerBoundsAreExact(t *testing.T) {
	type bounded struct {
		N int64 `query:"n" maximum:"9007199254740992"`
	}
	_, err := bind[bounded](t, "n=9007199254740993")
	if p := problemOf(t, err); p.Errors[0].Code != apicontract.FieldOutOfRange {
		t.Fatalf("got %+v", p)
	}
}

func TestTimestampsFollowRFC3339(t *testing.T) {
	type window struct {
		apicontract.Range
	}
	for _, value := range []string{"2026-11-14T9:00:00Z", "2026-11-14T09:00:00,5Z", "2026-11-14T09:00:00%2B24:00", "2026-11-14 09:00:00Z"} {
		_, err := bind[window](t, "from="+value)
		if p := problemOf(t, err); p.Status != http.StatusBadRequest {
			t.Fatalf("%s accepted: %+v", value, p)
		}
	}
	if _, err := bind[window](t, "from=2026-11-14T09:00:00.25%2B23:59"); err != nil {
		t.Fatal(err)
	}
	type event struct {
		At time.Time `json:"at"`
	}
	err := apicontract.BindJSONBytes([]byte(`{"at": "2026-11-14T9:00:00Z"}`), &event{})
	if p := problemOf(t, err); p.Status != http.StatusBadRequest {
		t.Fatalf("got %+v", p)
	}
}

func TestZoneRequiredAndDefault(t *testing.T) {
	type required struct {
		Location *time.Location `query:"tz" required:"true"`
	}
	type defaulted struct {
		apicontract.Range
		Location *time.Location `query:"tz" default:"Europe/Berlin"`
	}
	_, err := bind[required](t, "")
	if p := problemOf(t, err); p.Errors[0].Code != apicontract.FieldRequired {
		t.Fatalf("got %+v", p)
	}
	q, err := bind[defaulted](t, "from=2026-11-14")
	if err != nil || q.Location.String() != "Europe/Berlin" || q.From.Hour() != 0 || q.From.UTC().Hour() != 23 {
		t.Fatalf("got %v %v", q.From, err)
	}
}

func TestQueryListRules(t *testing.T) {
	type lists struct {
		Tags []string              `query:"tags" maxLength:"3" maxItems:"2"`
		Sort []apicontract.SortKey `query:"sort" maxItems:"1"`
	}
	for _, raw := range []string{"tags=abcdef", "tags=a,b,c", "sort=title,created_at"} {
		_, err := bind[lists](t, raw)
		if p := problemOf(t, err); p.Status != http.StatusUnprocessableEntity {
			t.Fatalf("%s: %+v", raw, p)
		}
	}
}

func TestIdempotencyKeyGrammar(t *testing.T) {
	cases := map[string]string{
		`"key with spaces ok"`:  "key with spaces ok",
		`"has \"escaped\" bit"`: `has "escaped" bit`,
		"abcdefgh":              "abcdefgh",
	}
	for header, want := range cases {
		request := httptest.NewRequest("POST", "/", nil)
		request.Header.Set(apicontract.IdempotencyHeader, header)
		got, err := apicontract.IdempotencyKey(request)
		if err != nil || got != want {
			t.Fatalf("%s: %q %v", header, got, err)
		}
	}
	for _, header := range []string{"abcdefgh,ijklmnop", `"abcdefgh", "ijklmnop"`, `"unterminated`, `"bad \x escape"`} {
		request := httptest.NewRequest("POST", "/", nil)
		request.Header.Set(apicontract.IdempotencyHeader, header)
		if _, err := apicontract.IdempotencyKey(request); err == nil {
			t.Fatalf("%s accepted", header)
		}
	}
}

func TestWriteJSONDoesNotCommitOnEncodingFailure(t *testing.T) {
	recorder := httptest.NewRecorder()
	if err := apicontract.WriteJSON(recorder, http.StatusCreated, math.NaN()); err == nil {
		t.Fatal("expected an encoding error")
	}
	if recorder.Code != http.StatusOK || recorder.Body.Len() != 0 || recorder.Header().Get("Content-Type") != "" {
		t.Fatalf("nothing must be written: %d %q", recorder.Code, recorder.Body.String())
	}
	_ = apicontract.WriteProblem(recorder, errors.New("x"))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("the handler must still be able to send a problem: %d", recorder.Code)
	}
	reset := httptest.NewRecorder()
	_ = apicontract.WriteJSON(reset, http.StatusResetContent, map[string]bool{"ok": true})
	if reset.Body.Len() != 0 {
		t.Fatalf("205 has no content: %q", reset.Body.String())
	}
}

func TestBodyLimits(t *testing.T) {
	type small struct {
		Title string `json:"title"`
	}
	large := []byte(`{"title": "` + strings.Repeat("x", apicontract.DefaultMaxBodyBytes) + `"}`)
	if p := problemOf(t, apicontract.BindJSONBytes(large, &small{})); p.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %+v", p)
	}
	for _, limit := range []int64{0, -1, math.MaxInt64} {
		err := apicontract.BindJSONBytes([]byte(`{}`), &small{}, apicontract.MaxBytes(limit))
		if err == nil || errors.As(err, new(*apicontract.Problem)) {
			t.Fatalf("MaxBytes(%d): expected a programming error, got %v", limit, err)
		}
	}
	if _, err := apicontract.BodySchema(nil); err == nil {
		t.Fatal("BodySchema(nil) must return an error")
	}
}

func TestRequiredPointerIsNotNullable(t *testing.T) {
	type body struct {
		Capacity *int `json:"capacity" required:"true"`
	}
	schema, err := apicontract.BodySchema(body{})
	if err != nil {
		t.Fatal(err)
	}
	if kind := schema["properties"].(map[string]any)["capacity"].(map[string]any)["type"]; kind != "integer" {
		t.Fatalf("got %v", kind)
	}
}

func TestSecondReviewRegressions(t *testing.T) {
	a := httptest.NewRequest("GET", "/projects/a%2Fb/events", nil)
	b := httptest.NewRequest("GET", "/projects/a/b/events", nil)
	if apicontract.RequestBinding(a, "u") == apicontract.RequestBinding(b, "u") {
		t.Fatal("escaped and unescaped paths must bind differently")
	}

	type withMap struct {
		Metadata map[string]any `json:"metadata"`
	}
	target := withMap{Metadata: map[string]any{"a": 1}}
	if err := apicontract.BindJSONBytes([]byte(`{"metadata": null}`), &target); err != nil || target.Metadata != nil {
		t.Fatalf("null must clear a map: %v %v", target.Metadata, err)
	}

	type clampedDefault struct {
		N int `json:"n" default:"300" maximum:"200" clamp:"true"`
	}
	type nested struct {
		Rows [][]struct {
			N int8 `json:"n" default:"128"`
		} `json:"rows"`
	}
	for _, bad := range []any{&clampedDefault{}, &nested{}} {
		if err := apicontract.BindJSONBytes([]byte(`{}`), bad); err == nil || errors.As(err, new(*apicontract.Problem)) {
			t.Fatalf("%T: expected a programming error, got %v", bad, err)
		}
	}

	type exponentBound struct {
		N int64 `query:"n" maximum:"9.007199254740992e15"`
	}
	_, err := bind[exponentBound](t, "n=9007199254740993")
	if p := problemOf(t, err); p.Errors[0].Code != apicontract.FieldOutOfRange {
		t.Fatalf("got %+v", p)
	}

	type Tags []string
	type small struct {
		Level int8  `json:"level"`
		Tags  Tags  `json:"tags" maxLength:"3" enum:"a,b"`
		Value any   `json:"value" required:"true"`
		List  []any `json:"list"`
		Big   int64 `json:"big" maximum:"9007199254740993"`
	}
	schema, err := apicontract.BodySchema(small{})
	if err != nil {
		t.Fatal(err)
	}
	properties, _ := json.Marshal(schema["properties"])
	for _, want := range []string{
		`"level":{"maximum":127,"minimum":-128,"type":"integer"}`,
		`"tags":{"items":{"enum":["a","b"],"maxLength":3,"type":"string"},"type":"array"}`,
		`"value":{"not":{"type":"null"}}`,
		`"big":{"maximum":9007199254740993,"type":"integer"}`,
	} {
		if !strings.Contains(string(properties), want) {
			t.Errorf("schema %s lacks %s", properties, want)
		}
	}
	var value small
	err = apicontract.BindJSONBytes([]byte(`{"tags": ["abcdef"], "value": 1, "list": [null, 2]}`), &value)
	if p := problemOf(t, err); p.Errors[0].Name != "tags[0]" {
		t.Fatalf("named string slices take entry rules: %+v", p)
	}
	if err := apicontract.BindJSONBytes([]byte(`{"value": 1, "list": [null, 2]}`), &value); err != nil || len(value.List) != 2 {
		t.Fatalf("free-form lists keep nulls: %v %v", value.List, err)
	}
	err = apicontract.BindJSONBytes([]byte(`{"value": null}`), &value)
	if p := problemOf(t, err); p.Errors[0].Code != apicontract.FieldRequired {
		t.Fatalf("got %+v", p)
	}
}

func TestThirdReviewBinding(t *testing.T) {
	type fractional struct {
		N int64            `json:"n" maximum:"9007199254740992.5"`
		M int64            `json:"m" minimum:"-0.5"`
		F float64          `json:"f" maximum:"9007199254740995"`
		R []map[string]any `json:"r"`
	}
	var value fractional
	err := apicontract.BindJSONBytes([]byte(`{"n": 9007199254740993}`), &value)
	if p := problemOf(t, err); p.Errors[0].Code != apicontract.FieldOutOfRange {
		t.Fatalf("got %+v", p)
	}
	if err := apicontract.BindJSONBytes([]byte(`{"m": 0, "r": [null, {"a": 1}]}`), &value); err != nil || value.R[0] != nil || value.R[1]["a"] == nil {
		t.Fatalf("got %v %v", value, err)
	}
	schema, err := apicontract.BodySchema(fractional{})
	if err != nil {
		t.Fatal(err)
	}
	properties, _ := json.Marshal(schema["properties"])
	for _, want := range []string{
		`"n":{"maximum":9007199254740992,"type":"integer"}`,
		`"m":{"minimum":0,"type":"integer"}`,
		`"f":{"maximum":9007199254740996,"type":"number"}`,
	} {
		if !strings.Contains(string(properties), want) {
			t.Errorf("schema %s lacks %s", properties, want)
		}
	}
}
