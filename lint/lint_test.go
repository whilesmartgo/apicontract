package lint_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/whilesmartgo/apicontract/lint"
)

func check(t *testing.T, name string) []lint.Violation {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	violations, err := lint.Check(name, data)
	if err != nil {
		t.Fatal(err)
	}
	return violations
}

type found struct{ rule, method, path, subject string }

func summarize(violations []lint.Violation) []found {
	out := make([]found, 0, len(violations))
	for _, v := range violations {
		out = append(out, found{v.Rule, v.Method, v.Path, v.Subject})
	}
	return out
}

func TestDriftingDocument(t *testing.T) {
	got := summarize(check(t, "drift.json"))
	want := []found{
		{lint.RuleContractVersion, "", "", ""},
		{lint.RuleErrorResponses, "GET", "/events", ""},
		{lint.RuleParamTime, "GET", "/events", "from"},
		{lint.RuleParamTime, "GET", "/events", "to"},
		{lint.RuleResponseSchema, "GET", "/events", ""},
		{lint.RuleListCursor, "GET", "/messages", "cursor"},
		{lint.RuleListEnvelope, "GET", "/messages", ""},
		{lint.RuleListLimit, "GET", "/messages", "limit"},
		{lint.RuleParamLegacy, "GET", "/messages", "page"},
		{lint.RuleParamLegacy, "GET", "/messages", "page_size"},
		{lint.RuleParamName, "GET", "/messages", "folderId"},
		{lint.RuleListCursor, "GET", "/tags", "cursor"},
		{lint.RuleListEnvelope, "GET", "/tags", ""},
		{lint.RuleListLimit, "GET", "/tags", "limit"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
}

func TestConformingDocument(t *testing.T) {
	if got := check(t, "conforming.json"); len(got) != 0 {
		t.Fatalf("unexpected violations: %v", got)
	}
}

func TestExceptions(t *testing.T) {
	violations := check(t, "drift.json")
	exceptions := []lint.Exception{
		{Spec: "drift.json", Rule: "*", Method: "GET", Path: "/messages", Reason: "moves with the mail rewrite"},
		{Spec: "drift.json", Rule: lint.RuleParamTime, Method: "GET", Path: "/events", Subject: "from", Reason: "r"},
		{Spec: "drift.json", Rule: lint.RuleListLimit, Method: "GET", Path: "/gone", Reason: "route was removed"},
		{Spec: "other.json", Rule: lint.RuleListLimit, Method: "GET", Path: "/x", Reason: "not checked in this run"},
	}
	open, accepted, stale := lint.Apply(violations, exceptions, []string{"drift.json"})
	if len(accepted) != 7 || len(open) != len(violations)-7 {
		t.Fatalf("accepted %d open %d", len(accepted), len(open))
	}
	if len(stale) != 1 || stale[0].Path != "/gone" {
		t.Fatalf("stale = %v", stale)
	}

	seeded := lint.Seed(violations, exceptions, []string{"drift.json"}, "predates the contract")
	open, _, stale = lint.Apply(violations, seeded, []string{"drift.json"})
	if len(open) != 0 || len(stale) != 0 {
		t.Fatalf("after seeding: open %v stale %v", open, stale)
	}
	if seeded[0].Reason != "moves with the mail rewrite" || seeded[0].Path != "/messages" {
		t.Fatalf("seeding must keep existing wildcard entries: %v", seeded[0])
	}
}
