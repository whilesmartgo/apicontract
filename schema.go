package apicontract

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

//go:embed schema/error.json
var errorSchema []byte

// InputSchema describes the query parameters of the struct value v as a JSON
// Schema object. Use it for tool input schemas so tools accept the same names,
// formats and bounds as the HTTP route.
func InputSchema(v any) (map[string]any, error) {
	p, err := planFor(reflect.TypeOf(v))
	if err != nil {
		return nil, err
	}
	properties := map[string]any{}
	for _, item := range p.params {
		properties[item.name] = item.schema()
	}
	required, alternatives := requirements(len(p.params), func(i int) (string, bool, string) {
		return p.params[i].name, p.params[i].required, p.params[i].replacement
	})
	return objectSchema(properties, required, alternatives), nil
}

// requirements lists required names and, for a required name that old names
// can stand in for, a set of alternatives any one of which satisfies it.
func requirements(n int, at func(int) (name string, required bool, replacement string)) ([]string, [][]string) {
	aliases := map[string][]string{}
	for i := 0; i < n; i++ {
		if name, _, replacement := at(i); replacement != "" {
			aliases[replacement] = append(aliases[replacement], name)
		}
	}
	required := []string{}
	var alternatives [][]string
	for i := 0; i < n; i++ {
		name, isRequired, _ := at(i)
		if !isRequired {
			continue
		}
		if old := aliases[name]; len(old) > 0 {
			alternatives = append(alternatives, append([]string{name}, old...))
			continue
		}
		required = append(required, name)
	}
	return required, alternatives
}

func objectSchema(properties map[string]any, required []string, alternatives [][]string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	var all []any
	for _, names := range alternatives {
		var options []any
		for _, name := range names {
			options = append(options, map[string]any{
				"required":   []string{name},
				"properties": map[string]any{name: map[string]any{"not": map[string]any{"type": "null"}}},
			})
		}
		all = append(all, map[string]any{"anyOf": options})
	}
	if len(all) > 0 {
		schema["allOf"] = all
	}
	return schema
}

// BodySchema describes the body struct value v as a JSON Schema object, as
// BindJSON reads it. Use it for tool input schemas and request body schemas.
func BodySchema(v any) (map[string]any, error) {
	t := reflect.TypeOf(v)
	if t == nil || t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("apicontract: %v is not a struct", t)
	}
	p, err := bodyPlanFor(reflect.New(t).Interface())
	if err != nil {
		return nil, err
	}
	return p.schema(), nil
}

// OpenAPIParameters describes the parameters of the struct value v as OpenAPI
// 3.1 query parameter objects, for specs that are built in code.
func OpenAPIParameters(v any) ([]map[string]any, error) {
	p, err := planFor(reflect.TypeOf(v))
	if err != nil {
		return nil, err
	}
	return p.openAPI(), nil
}

// OpenAPIParametersOf is OpenAPIParameters for a struct described by fields
// instead of a value.
func OpenAPIParametersOf(fields []Field) ([]map[string]any, error) {
	p, err := planOf(fields)
	if err != nil {
		return nil, err
	}
	return p.openAPI(), nil
}

// OpenAPIRequestBody describes the body struct value v as an OpenAPI 3.1
// request body object.
func OpenAPIRequestBody(v any) (map[string]any, error) {
	schema, err := BodySchema(v)
	if err != nil {
		return nil, err
	}
	return requestBody(schema), nil
}

// OpenAPIRequestBodyOf is OpenAPIRequestBody for a struct described by fields
// instead of a value.
func OpenAPIRequestBodyOf(fields []Field) (map[string]any, error) {
	p, err := bodyPlanOf(fields, "")
	if err != nil {
		return nil, err
	}
	return requestBody(p.schema()), nil
}

func requestBody(schema map[string]any) map[string]any {
	return map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": schema}}}
}

// ErrorSchema returns the JSON Schema of the problem body, as published in
// schema/error.json, without its $schema and $id keywords.
func ErrorSchema() map[string]any {
	var schema map[string]any
	if err := json.Unmarshal(errorSchema, &schema); err != nil {
		panic(err)
	}
	delete(schema, "$schema")
	delete(schema, "$id")
	return schema
}

// OpenAPIProblemResponse returns an OpenAPI response object for an error
// status, with the problem body under application/problem+json.
func OpenAPIProblemResponse(status int) map[string]any {
	return map[string]any{
		"description": http.StatusText(status),
		"content":     map[string]any{ProblemContentType: map[string]any{"schema": ErrorSchema()}},
	}
}

func (p *plan) openAPI() []map[string]any {
	parameters := make([]map[string]any, 0, len(p.params))
	for _, item := range p.params {
		parameter := map[string]any{"name": item.name, "in": "query", "schema": item.schema()}
		if item.doc != "" {
			parameter["description"] = item.doc
		}
		if item.required {
			if old := p.aliasesOf(item.name); len(old) > 0 {
				parameter["description"] = strings.TrimSpace(item.doc + " Required unless " + strings.Join(old, " or ") + " is sent.")
			} else {
				parameter["required"] = true
			}
		}
		if item.deprecated {
			parameter["deprecated"] = true
		}
		parameters = append(parameters, parameter)
	}
	return parameters
}

func (p *plan) aliasesOf(name string) []string {
	var old []string
	for _, item := range p.params {
		if item.replacement == name {
			old = append(old, item.name)
		}
	}
	return old
}

func (item param) schema() map[string]any {
	schema := map[string]any{}
	switch item.typ {
	case FieldString:
		stringSchema(schema, item.rules)
	case FieldInt:
		schema["type"] = "integer"
		bounds(schema, item.rules)
	case FieldBool:
		schema["type"] = "boolean"
	case FieldTime:
		schema["type"] = "string"
		schema["anyOf"] = []any{map[string]any{"format": "date-time"}, map[string]any{"format": "date"}}
	case FieldDate:
		schema["type"] = "string"
		schema["format"] = "date"
	case FieldLocation:
		schema["type"] = "string"
	case FieldStrings:
		schema["type"] = "string"
		if len(item.enum) > 0 {
			schema["pattern"] = "^(" + alternation(item.enum) + ")(,(" + alternation(item.enum) + "))*$"
		}
	case FieldSort:
		schema["type"] = "string"
		fields := "[a-z][a-z0-9_]*"
		if len(item.enum) > 0 {
			fields = "(" + alternation(item.enum) + ")"
		}
		schema["pattern"] = "^-?" + fields + "(,-?" + fields + ")*$"
	}
	if item.defaultRaw != "" {
		switch item.typ {
		case FieldInt:
			if number, err := strconv.Atoi(item.defaultRaw); err == nil {
				schema["default"] = number
			}
		case FieldBool:
			if flag, err := strconv.ParseBool(item.defaultRaw); err == nil {
				schema["default"] = flag
			}
		default:
			schema["default"] = item.defaultRaw
		}
	}
	annotate(schema, item.rules)
	return schema
}

func (p *bodyPlan) schema() map[string]any {
	properties := map[string]any{}
	for _, field := range p.fields {
		properties[field.name] = field.schema()
	}
	required, alternatives := requirements(len(p.fields), func(i int) (string, bool, string) {
		return p.fields[i].name, p.fields[i].required, p.fields[i].replacement
	})
	return objectSchema(properties, required, alternatives)
}

func (field bodyField) schema() map[string]any {
	schema := map[string]any{}
	switch field.typ {
	case FieldString:
		stringSchema(schema, field.rules)
	case FieldInt:
		schema["type"] = "integer"
		bounds(schema, field.rules)
	case FieldNumber:
		schema["type"] = "number"
		bounds(schema, field.rules)
	case FieldBool:
		schema["type"] = "boolean"
	case FieldTime:
		schema["type"] = "string"
		schema["format"] = "date-time"
	case FieldDate:
		schema["type"] = "string"
		schema["format"] = "date"
	case FieldStrings:
		schema["type"] = "array"
		items := map[string]any{}
		stringSchema(items, rules{maxLength: field.maxLength, enum: field.enum})
		schema["items"] = items
	case FieldList:
		schema["type"] = "array"
		schema["items"] = field.elem.schema()
	case FieldStruct:
		schema = field.nested.schema()
	case FieldObject:
		schema["type"] = "object"
	}
	if field.maxItems > 0 {
		schema["maxItems"] = field.maxItems
	}
	if field.defaultRaw != "" {
		if value, err := field.parseDefault(); err == nil {
			schema["default"] = value
		}
	}
	if field.required && field.typ == FieldAny {
		schema["not"] = map[string]any{"type": "null"}
	}
	if field.nullable() && !field.required {
		if kind, ok := schema["type"].(string); ok {
			schema["type"] = []any{kind, "null"}
		}
		if enum, ok := schema["enum"].([]string); ok {
			values := make([]any, 0, len(enum)+1)
			for _, value := range enum {
				values = append(values, value)
			}
			schema["enum"] = append(values, nil)
		}
	}
	annotate(schema, field.rules)
	return schema
}

func stringSchema(schema map[string]any, r rules) {
	schema["type"] = "string"
	if r.maxLength > 0 {
		schema["maxLength"] = r.maxLength
	}
	if len(r.enum) > 0 {
		schema["enum"] = r.enum
	}
}

func bounds(schema map[string]any, r rules) {
	integer := schema["type"] == "integer"
	if integer && r.minInt != nil {
		schema["minimum"] = integerValue(*r.minInt)
	} else if r.minimum != nil {
		schema["minimum"] = numberValue(*r.minimum)
	}
	if integer && r.maxInt != nil {
		schema["maximum"] = integerValue(*r.maxInt)
	} else if r.maximum != nil {
		schema["maximum"] = numberValue(*r.maximum)
	}
	if !integer || r.bits <= 0 || r.bits >= 64 {
		return
	}
	low, high := integerRange(r.bits)
	if r.minimum == nil || *r.minimum < float64(low) {
		schema["minimum"] = integerValue(low)
	}
	if r.maximum == nil || *r.maximum > float64(high) {
		schema["maximum"] = integerValue(high)
	}
}

func integerValue(number int64) any {
	if number >= math.MinInt && number <= math.MaxInt {
		return int(number)
	}
	return number
}

func numberValue(number float64) any {
	if number == math.Trunc(number) && math.Abs(number) < 1<<53 {
		return int(number)
	}
	return number
}

func annotate(schema map[string]any, r rules) {
	if r.doc != "" {
		schema["description"] = r.doc
	}
	if r.deprecated {
		schema["deprecated"] = true
		if r.replacement != "" {
			schema["description"] = strings.TrimSpace(r.doc + " Deprecated: use " + r.replacement + ".")
		}
	}
}

func alternation(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = regexp.QuoteMeta(value)
	}
	return strings.Join(quoted, "|")
}
