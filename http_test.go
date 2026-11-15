package apicontract_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/whilesmartgo/apicontract"
)

type item struct {
	ID int `json:"id"`
}

func handler(cursors *apicontract.Cursors) http.Handler {
	all := make([]item, 7)
	for i := range all {
		all[i] = item{ID: i + 1}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct{ apicontract.Page }
		if err := apicontract.BindQuery(r, &q); err != nil {
			apicontract.WriteProblem(w, err)
			return
		}
		binding := apicontract.QueryBinding(r.URL.Query(), r.Header.Get("X-User"))
		var position struct{ After int }
		if q.Cursor != "" {
			if err := cursors.Decode(q.Cursor, binding, &position); err != nil {
				apicontract.WriteProblem(w, err)
				return
			}
		}
		end := min(position.After+q.Limit, len(all))
		next := ""
		if end < len(all) {
			next, _ = cursors.Encode(binding, struct{ After int }{end})
		}
		apicontract.WriteList(w, all[position.After:end], next)
	})
}

func get(t *testing.T, h http.Handler, target, user string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Header.Set("X-User", user)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: %v", recorder.Body.String(), err)
	}
	return recorder, body
}

func TestCursorPagingThroughHTTP(t *testing.T) {
	cursors, err := apicontract.NewCursors([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	h := handler(cursors)
	seen := map[float64]bool{}
	target := "/items?limit=3"
	for pages := 0; ; pages++ {
		recorder, body := get(t, h, target, "u1")
		if recorder.Code != http.StatusOK {
			t.Fatalf("status %d: %v", recorder.Code, body)
		}
		for _, entry := range body["items"].([]any) {
			id := entry.(map[string]any)["id"].(float64)
			if seen[id] {
				t.Fatalf("item %v returned twice", id)
			}
			seen[id] = true
		}
		next, _ := body["next_cursor"].(string)
		if next == "" {
			break
		}
		target = "/items?limit=3&cursor=" + next
	}
	if len(seen) != 7 {
		t.Fatalf("saw %d items, want 7", len(seen))
	}
}

func TestCursorIsBoundToPrincipal(t *testing.T) {
	cursors, _ := apicontract.NewCursors([]byte(strings.Repeat("k", 32)))
	h := handler(cursors)
	_, body := get(t, h, "/items?limit=3", "u1")
	recorder, body := get(t, h, "/items?limit=3&cursor="+body["next_cursor"].(string), "u2")
	if recorder.Code != http.StatusBadRequest || body["code"] != apicontract.CodeMalformedQuery {
		t.Fatalf("status %d body %v", recorder.Code, body)
	}
	if _, ok := body["title"].(string); !ok || recorder.Header().Get("Content-Type") != apicontract.ProblemContentType {
		t.Fatalf("problems must be RFC 9457 bodies: %v", body)
	}
}

func TestEmptyListIsAnArray(t *testing.T) {
	recorder := httptest.NewRecorder()
	apicontract.WriteList[item](recorder, nil, "")
	if got := strings.TrimSpace(recorder.Body.String()); got != `{"items":[]}` {
		t.Fatalf("got %s", got)
	}
}

func TestUnknownErrorsDoNotLeak(t *testing.T) {
	recorder := httptest.NewRecorder()
	apicontract.WriteProblem(recorder, errors.New("pq: password authentication failed"))
	if recorder.Code != http.StatusInternalServerError || strings.Contains(recorder.Body.String(), "password") {
		t.Fatalf("status %d body %s", recorder.Code, recorder.Body.String())
	}
}
