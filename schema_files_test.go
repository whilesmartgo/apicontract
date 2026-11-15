package apicontract_test

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/whilesmartgo/apicontract"
)

func TestSchemaFilesMatchPackage(t *testing.T) {
	type shared struct {
		apicontract.Page
		apicontract.Range
		apicontract.Zone
		apicontract.Search
		Sort []apicontract.SortKey `query:"sort" doc:"Comma-separated fields. A leading - sorts that field descending."`
	}
	generated, err := apicontract.InputSchema(shared{})
	if err != nil {
		t.Fatal(err)
	}
	published := readSchema(t, "schema/query.json")["properties"].(map[string]any)
	properties := normalize(t, generated["properties"])
	if !reflect.DeepEqual(properties, published) {
		got, _ := json.MarshalIndent(properties, "", "  ")
		t.Fatalf("schema/query.json differs from the package; the package produces:\n%s", got)
	}
}

func normalize(t *testing.T, value any) map[string]any {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestErrorSchemaMatchesPackage(t *testing.T) {
	schema := readSchema(t, "schema/error.json")
	errorsProperty := schema["properties"].(map[string]any)["errors"].(map[string]any)
	itemProperties := errorsProperty["items"].(map[string]any)["properties"].(map[string]any)
	codes := itemProperties["code"].(map[string]any)["enum"].([]any)
	want := []any{apicontract.FieldInvalid, apicontract.FieldRequired, apicontract.FieldOutOfRange, apicontract.FieldTooLong, apicontract.FieldTooMany, apicontract.FieldUnknownValue, apicontract.FieldUnknown, apicontract.FieldConflict, apicontract.FieldInvalidCursor}
	if !reflect.DeepEqual(codes, want) {
		t.Fatalf("schema/error.json field codes %v, package %v", codes, want)
	}
	in := itemProperties["in"].(map[string]any)["enum"].([]any)
	if !reflect.DeepEqual(in, []any{apicontract.InQuery, apicontract.InBody, apicontract.InHeader}) {
		t.Fatalf("schema/error.json in values %v", in)
	}
	body := normalize(t, apicontract.NewProblem(422, apicontract.CodeInvalidBody, "d", apicontract.FieldError{In: "body", Name: "n", Code: "c", Detail: "m"}))
	for key := range body {
		if _, ok := schema["properties"].(map[string]any)[key]; !ok {
			t.Fatalf("Problem writes %q, which schema/error.json does not declare", key)
		}
	}
	entry := normalize(t, apicontract.FieldError{})
	for key := range entry {
		if _, ok := itemProperties[key]; !ok {
			t.Fatalf("FieldError writes %q, which schema/error.json does not declare", key)
		}
	}
}

func TestEveryProblemCodeIsDocumented(t *testing.T) {
	docs, err := os.ReadFile("docs/errors.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{apicontract.CodeMalformedQuery, apicontract.CodeInvalidQuery, apicontract.CodeMalformedBody, apicontract.CodeInvalidBody, apicontract.CodeUnsupportedMediaType, apicontract.CodeBodyTooLarge, apicontract.CodeMalformedHeader, apicontract.CodeInternal} {
		if !strings.Contains(string(docs), "\n## "+code+"\n") {
			t.Errorf("docs/errors.md has no section for %s", code)
		}
		if problem := apicontract.NewProblem(400, code, ""); problem.Type != apicontract.ProblemTypeBase+code {
			t.Errorf("%s has type %q", code, problem.Type)
		}
	}
}
