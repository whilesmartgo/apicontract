package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLintExitCodes(t *testing.T) {
	dir := t.TempDir()
	spec := filepath.Join(dir, "drift.json")
	data, err := os.ReadFile("../../lint/testdata/drift.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec, data, 0o644); err != nil {
		t.Fatal(err)
	}
	exceptions := filepath.Join(dir, "exceptions.json")
	report := filepath.Join(dir, "report.md")

	lint := func(args ...string) (int, string) {
		var out, errOut bytes.Buffer
		code := run(append([]string{"lint"}, args...), &out, &errOut)
		return code, out.String() + errOut.String()
	}

	if code, out := lint(spec); code != 1 || !strings.Contains(out, "param-legacy") {
		t.Fatalf("drift must fail: %d %s", code, out)
	}
	if code, out := lint("-exceptions", exceptions, "-seed", "predates the contract", spec); code != 0 {
		t.Fatalf("seeding must accept every violation: %d %s", code, out)
	}
	if code, out := lint("-exceptions", exceptions, "-report", report, spec); code != 0 {
		t.Fatalf("seeded run must pass: %d %s", code, out)
	}
	table, _ := os.ReadFile(report)
	if !strings.Contains(string(table), "| `"+spec+"` | `GET /messages` | param-legacy | `page` | predates the contract |") {
		t.Fatalf("report:\n%s", table)
	}

	fixed := strings.Replace(string(data), `"page_size"`, `"limit"`, 1)
	if err := os.WriteFile(spec, []byte(fixed), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := lint("-exceptions", exceptions, spec); code != 1 || !strings.Contains(out, "stale exception") {
		t.Fatalf("a fixed route must fail until its exception is removed: %d %s", code, out)
	}
}
