package apicontract_test

import (
	"encoding/json"
	"reflect"
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
	fields := schema["properties"].(map[string]any)["fields"].(map[string]any)
	codes := fields["items"].(map[string]any)["properties"].(map[string]any)["code"].(map[string]any)["enum"].([]any)
	want := []any{apicontract.FieldInvalid, apicontract.FieldRequired, apicontract.FieldOutOfRange, apicontract.FieldTooLong, apicontract.FieldUnknownValue, apicontract.FieldConflict, apicontract.FieldInvalidCursor}
	if !reflect.DeepEqual(codes, want) {
		t.Fatalf("schema/error.json field codes %v, package %v", codes, want)
	}
	body := normalize(t, apicontract.ErrorBody{Error: "e", Code: "c", Fields: []apicontract.FieldError{{Name: "n", Code: "c", Message: "m"}}})
	for key := range body {
		if _, ok := schema["properties"].(map[string]any)[key]; !ok {
			t.Fatalf("ErrorBody writes %q, which schema/error.json does not declare", key)
		}
	}
}
