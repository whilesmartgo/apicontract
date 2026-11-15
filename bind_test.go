package apicontract_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/whilesmartgo/apicontract"
)

type eventsQuery struct {
	apicontract.Page
	apicontract.Range
	apicontract.Zone
	apicontract.Search
	Sort     []apicontract.SortKey `query:"sort" enum:"starts_at,title"`
	Status   []string              `query:"status" enum:"open,closed"`
	PageSize int                   `query:"page_size" deprecated:"limit"`
	Start    time.Time             `query:"start" deprecated:"from"`
	End      time.Time             `query:"end" deprecated:"to" bound:"end"`
}

type notesQuery struct {
	apicontract.Page
	Updated apicontract.Range `query:"updated"`
	Linked  bool              `query:"linked"`
	Owner   string            `query:"owner_id" required:"true"`
}

func bind[T any](t *testing.T, raw string) (T, error) {
	t.Helper()
	var target T
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	return target, apicontract.BindValues(values, &target)
}

func problemOf(t *testing.T, err error) *apicontract.Problem {
	t.Helper()
	var problem *apicontract.Problem
	if !errors.As(err, &problem) {
		t.Fatalf("expected *Problem, got %v", err)
	}
	return problem
}

func TestDefaultsAndClamp(t *testing.T) {
	q, err := bind[eventsQuery](t, "")
	if err != nil || q.Limit != apicontract.DefaultLimit {
		t.Fatalf("limit = %d, err = %v", q.Limit, err)
	}
	q, err = bind[eventsQuery](t, "limit=500")
	if err != nil || q.Limit != apicontract.MaxLimit {
		t.Fatalf("limit = %d, err = %v", q.Limit, err)
	}
}

func TestMalformedIs400AndRuleBreakIs422(t *testing.T) {
	_, err := bind[eventsQuery](t, "limit=ten")
	if p := problemOf(t, err); p.Status != http.StatusBadRequest || p.Errors[0].Name != "limit" || p.Errors[0].Code != apicontract.FieldInvalid {
		t.Fatalf("got %+v", p)
	}
	_, err = bind[eventsQuery](t, "limit=0")
	if p := problemOf(t, err); p.Status != http.StatusUnprocessableEntity || p.Errors[0].Code != apicontract.FieldOutOfRange {
		t.Fatalf("got %+v", p)
	}
	_, err = bind[eventsQuery](t, "from=2026-11-14T10:00:00")
	if p := problemOf(t, err); p.Status != http.StatusBadRequest {
		t.Fatalf("timestamp without offset accepted: %+v", p)
	}
	_, err = bind[eventsQuery](t, "limit=0&from=nope")
	if p := problemOf(t, err); p.Status != http.StatusBadRequest || len(p.Errors) != 2 {
		t.Fatalf("a malformed value must make the whole problem a 400 listing every field: %+v", p)
	}
}

func TestRangeAcceptsDaysAndTimestamps(t *testing.T) {
	q, err := bind[eventsQuery](t, "from=2026-11-14&to=2026-11-14&tz=Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	berlin, _ := time.LoadLocation("Europe/Berlin")
	if !q.From.Equal(time.Date(2026, 11, 14, 0, 0, 0, 0, berlin)) || !q.To.Equal(time.Date(2026, 11, 15, 0, 0, 0, 0, berlin)) {
		t.Fatalf("from %s to %s", q.From, q.To)
	}
	q, err = bind[eventsQuery](t, "from=2026-11-14T08:00:00Z&to=2026-11-14T10:00:00%2B01:00")
	if err != nil {
		t.Fatal(err)
	}
	if q.To.Sub(q.From) != time.Hour {
		t.Fatalf("timestamps must keep their offsets: %s %s", q.From, q.To)
	}
	_, err = bind[eventsQuery](t, "from=2026-11-15&to=2026-11-14")
	if p := problemOf(t, err); p.Status != http.StatusUnprocessableEntity || p.Errors[0].Name != "to" {
		t.Fatalf("got %+v", p)
	}
	_, err = bind[eventsQuery](t, "tz=Local")
	if p := problemOf(t, err); p.Status != http.StatusBadRequest || p.Errors[0].Name != "tz" {
		t.Fatalf("got %+v", p)
	}
}

func TestPrefixedRange(t *testing.T) {
	q, err := bind[notesQuery](t, "owner_id=u1&updated_from=2026-01-01&updated_to=2026-01-31&linked=true")
	if err != nil {
		t.Fatal(err)
	}
	if q.Updated.From.IsZero() || !q.Updated.To.Equal(time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)) || !q.Linked {
		t.Fatalf("got %+v", q)
	}
	_, err = bind[notesQuery](t, "")
	if p := problemOf(t, err); p.Status != http.StatusUnprocessableEntity || p.Errors[0].Code != apicontract.FieldRequired {
		t.Fatalf("got %+v", p)
	}
}

func TestDeprecatedAliases(t *testing.T) {
	q, err := bind[eventsQuery](t, "page_size=20&start=2026-11-01&end=2026-11-30")
	if err != nil {
		t.Fatal(err)
	}
	if q.Limit != 20 || q.PageSize != 20 {
		t.Fatalf("limit %d page_size %d", q.Limit, q.PageSize)
	}
	if !q.To.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("end alias must take the end bound of to: %s", q.To)
	}
	_, err = bind[eventsQuery](t, "page_size=20&limit=30")
	if p := problemOf(t, err); p.Status != http.StatusUnprocessableEntity || p.Errors[0].Code != apicontract.FieldConflict {
		t.Fatalf("got %+v", p)
	}
	_, err = bind[eventsQuery](t, "page_size=many")
	if p := problemOf(t, err); p.Errors[0].Name != "page_size" {
		t.Fatalf("errors must name the parameter the caller sent: %+v", p)
	}
}

func TestSortAndLists(t *testing.T) {
	q, err := bind[eventsQuery](t, "sort=-starts_at,title&status=open,closed")
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Sort) != 2 || !q.Sort[0].Desc || q.Sort[1].Field != "title" || len(q.Status) != 2 {
		t.Fatalf("got %+v", q)
	}
	_, err = bind[eventsQuery](t, "sort=owner")
	if p := problemOf(t, err); p.Status != http.StatusUnprocessableEntity {
		t.Fatalf("got %+v", p)
	}
	_, err = bind[eventsQuery](t, "sort=title,,starts_at")
	if p := problemOf(t, err); p.Status != http.StatusBadRequest {
		t.Fatalf("got %+v", p)
	}
}

func TestInvalidStructs(t *testing.T) {
	type duplicate struct {
		apicontract.Page
		Limit2 int `query:"limit"`
	}
	type dangling struct {
		Old int `query:"old" deprecated:"new"`
	}
	type unsupported struct {
		Ratio float64 `query:"ratio"`
	}
	for _, target := range []any{&duplicate{}, &dangling{}, &unsupported{}, duplicate{}} {
		err := apicontract.BindValues(url.Values{}, target)
		var problem *apicontract.Problem
		if err == nil || errors.As(err, &problem) {
			t.Fatalf("%T: expected a programming error, got %v", target, err)
		}
	}
}

func TestUndeclaredAndRepeatedParameters(t *testing.T) {
	_, err := bind[eventsQuery](t, "limt=20&colour=red")
	p := problemOf(t, err)
	if p.Status != http.StatusUnprocessableEntity || p.Code != apicontract.CodeInvalidQuery || len(p.Errors) != 2 || p.Errors[0].Name != "colour" || p.Errors[1].Name != "limt" || p.Errors[0].Code != apicontract.FieldUnknown {
		t.Fatalf("got %+v", p)
	}
	_, err = bind[eventsQuery](t, "limit=20&limit=30")
	if p := problemOf(t, err); p.Status != http.StatusBadRequest || p.Errors[0].Name != "limit" {
		t.Fatalf("got %+v", p)
	}
	request := httptest.NewRequest(http.MethodGet, "/events?q=%zz", nil)
	if p := problemOf(t, apicontract.BindQuery(request, &eventsQuery{})); p.Status != http.StatusBadRequest {
		t.Fatalf("got %+v", p)
	}
}

func TestDateParameter(t *testing.T) {
	type dayQuery struct {
		Day apicontract.Date `query:"day" required:"true"`
	}
	q, err := bind[dayQuery](t, "day=2026-11-14")
	if err != nil || q.Day.String() != "2026-11-14" {
		t.Fatalf("got %v %v", q.Day, err)
	}
	_, err = bind[dayQuery](t, "day=2026-02-30")
	if p := problemOf(t, err); p.Status != http.StatusBadRequest {
		t.Fatalf("got %+v", p)
	}
}
