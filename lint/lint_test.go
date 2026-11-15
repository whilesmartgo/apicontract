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
		{lint.RuleErrorMediaType, "GET", "/messages", "400"},
		{lint.RuleListCursor, "GET", "/messages", "cursor"},
		{lint.RuleListEnvelope, "GET", "/messages", ""},
		{lint.RuleListLimit, "GET", "/messages", "limit"},
		{lint.RuleParamLegacy, "GET", "/messages", "page"},
		{lint.RuleParamLegacy, "GET", "/messages", "page_size"},
		{lint.RuleParamName, "GET", "/messages", "folderId"},
		{lint.RuleBodyClosed, "POST", "/notes", "(body)"},
		{lint.RuleBodyClosed, "POST", "/notes", "labels[]"},
		{lint.RuleBodyName, "POST", "/notes", "labels[].colourHex"},
		{lint.RuleBodyName, "POST", "/notes", "noteTitle"},
		{lint.RuleBodyTime, "POST", "/notes", "remind_at"},
		{lint.RuleErrorMediaType, "POST", "/notes", "422"},
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
	if len(accepted) != 8 || len(open) != len(violations)-8 {
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

func TestStricterShapes(t *testing.T) {
	document := `{
	  "openapi": "3.1.0",
	  "info": {"title": "t", "version": "1", "x-whilesmart-contract": "0.1"},
	  "paths": {
	    "/composed": {"get": {"responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"allOf": [{"type": "array", "items": {"type": "string"}}]}}}}}}},
	    "/nullable": {"get": {"responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"type": "object", "required": ["items"], "properties": {"items": {"type": ["array", "null"]}}}}}}}}},
	    "/probe": {"head": {"parameters": [{"name": "badName", "in": "query", "schema": {"type": "string"}}], "responses": {"200": {"description": "ok"}, "400": {"description": "bad", "content": {"application/problem+json": {}}}, "500": {"description": "failed", "content": {"application/json": {}}}}}},
	    "/notes": {"post": {
	      "requestBody": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Base", "additionalProperties": false, "properties": {"badName": {"type": "string"}}}}}},
	      "responses": {"201": {"description": "ok"}, "422": {"description": "bad", "content": {"application/problem+json": {}}}}
	    }}
	  },
	  "components": {"schemas": {"Base": {"type": "object"}}}
	}`
	violations, err := lint.Check("strict", []byte(document))
	if err != nil {
		t.Fatal(err)
	}
	got := summarize(violations)
	want := []found{
		{lint.RuleListCursor, "GET", "/composed", "cursor"},
		{lint.RuleListEnvelope, "GET", "/composed", ""},
		{lint.RuleListLimit, "GET", "/composed", "limit"},
		{lint.RuleBodyName, "POST", "/notes", "badName"},
		{lint.RuleListCursor, "GET", "/nullable", "cursor"},
		{lint.RuleListEnvelope, "GET", "/nullable", ""},
		{lint.RuleListLimit, "GET", "/nullable", "limit"},
		{lint.RuleErrorMediaType, "HEAD", "/probe", "500"},
		{lint.RuleParamName, "HEAD", "/probe", "badName"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
}

func TestReferencesAndComposition(t *testing.T) {
	document := `{
	  "openapi": "3.1.0",
	  "info": {"title": "t", "version": "1", "x-whilesmart-contract": "0.1"},
	  "paths": {
	    "/a": {"post": {
	      "requestBody": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Base", "properties": {"good_name": {"type": "string"}}}}}},
	      "responses": {"201": {"description": "ok"}, "422": {"description": "bad", "content": {"application/problem+json": {}}}}
	    }},
	    "/b": {"post": {
	      "requestBody": {"content": {"application/json": {"schema": {"type": "object", "additionalProperties": false, "properties": {"created_at": {"type": "string", "format": "date"}}}}}},
	      "responses": {"201": {"description": "ok"}, "422": {"description": "bad", "content": {"application/problem+json": {}}}}
	    }},
	    "/c": {"get": {"responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"anyOf": [{"type": "array", "items": {}}]}}}}}}},
	    "/d": {"get": {"responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"type": "object", "required": ["items"], "properties": {"items": {"type": ["array", "string"]}}}}}}}}},
	    "/e": {"get": {"responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"$ref": "#/components/schemas/S"}}}}}}}
	  },
	  "components": {"schemas": {
	    "Base": {"type": "object", "additionalProperties": false, "properties": {"badName": {"type": "string"}}},
	    "S": {"allOf": [{"$ref": "#/components/schemas/S"}, {"$ref": "#/components/schemas/S"}, {"$ref": "#/components/schemas/S"}]}
	  }}
	}`
	violations, err := lint.Check("refs", []byte(document))
	if err != nil {
		t.Fatal(err)
	}
	got := summarize(violations)
	want := []found{
		{lint.RuleBodyName, "POST", "/a", "badName"},
		{lint.RuleBodyTime, "POST", "/b", "created_at"},
		{lint.RuleListCursor, "GET", "/c", "cursor"},
		{lint.RuleListEnvelope, "GET", "/c", ""},
		{lint.RuleListLimit, "GET", "/c", "limit"},
		{lint.RuleListCursor, "GET", "/d", "cursor"},
		{lint.RuleListEnvelope, "GET", "/d", ""},
		{lint.RuleListLimit, "GET", "/d", "limit"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
}

func TestThirdReviewLint(t *testing.T) {
	problem := `"422": {"description": "bad", "content": {"application/problem+json": {}}}`
	document := `{
	  "openapi": "3.1.0",
	  "info": {"title": "t", "version": "1", "x-whilesmart-contract": "0.1"},
	  "paths": {
	    "/tree": {"post": {"requestBody": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Node"}}}}, "responses": {` + problem + `}}},
	    "/closed": {"post": {"requestBody": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Base", "properties": {"name": {"type": "string"}}, "additionalProperties": false}}}}, "responses": {` + problem + `}}},
	    "/untyped": {"post": {"requestBody": {"content": {"application/json": {"schema": {"properties": {"name": {"type": "string"}}}}}}, "responses": {` + problem + `}}},
	    "/limited": {"get": {
	      "parameters": [
	        {"name": "limit", "in": "query", "schema": {"$ref": "#/components/schemas/Limit", "maximum": 200}},
	        {"name": "cursor", "in": "query", "schema": {"type": "string"}}
	      ],
	      "responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"type": "object", "required": ["items"], "properties": {"items": {"type": "array", "items": {}}}}}}}, "400": {"description": "bad", "content": {"application/problem+json": {}}}}
	    }},
	    "/nested": {"get": {"responses": {"200": {"description": "ok", "content": {"application/json": {"schema": {"anyOf": [{"anyOf": [{"type": "array", "items": {"type": "string"}}]}]}}}}}}}
	  },
	  "components": {"schemas": {
	    "Node": {"type": "object", "additionalProperties": false, "properties": {"child": {"$ref": "#/components/schemas/Node"}}},
	    "Base": {"type": "object", "properties": {"name": {"type": "string"}}},
	    "Limit": {"type": "integer"}
	  }}
	}`
	violations, err := lint.Check("third", []byte(document))
	if err != nil {
		t.Fatal(err)
	}
	got := summarize(violations)
	want := []found{
		{lint.RuleListCursor, "GET", "/nested", "cursor"},
		{lint.RuleListEnvelope, "GET", "/nested", ""},
		{lint.RuleListLimit, "GET", "/nested", "limit"},
		{lint.RuleBodyClosed, "POST", "/untyped", "(body)"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
}
