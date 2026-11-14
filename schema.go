package apicontract

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// InputSchema describes the parameters of the struct value v as a JSON Schema
// object. Use it for tool input schemas so tools accept the same names,
// formats and bounds as the HTTP route.
func InputSchema(v any) (map[string]any, error) {
	p, err := planFor(reflect.TypeOf(v))
	if err != nil {
		return nil, err
	}
	properties := map[string]any{}
	required := []string{}
	for _, item := range p.params {
		properties[item.name] = item.schema()
		if item.required {
			required = append(required, item.name)
		}
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema, nil
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

func (p *plan) openAPI() []map[string]any {
	parameters := make([]map[string]any, 0, len(p.params))
	for _, item := range p.params {
		parameter := map[string]any{"name": item.name, "in": "query", "schema": item.schema()}
		if item.doc != "" {
			parameter["description"] = item.doc
		}
		if item.required {
			parameter["required"] = true
		}
		if item.deprecated {
			parameter["deprecated"] = true
		}
		parameters = append(parameters, parameter)
	}
	return parameters
}

func (item param) schema() map[string]any {
	schema := map[string]any{}
	switch item.typ {
	case FieldString:
		schema["type"] = "string"
		if item.maxLength > 0 {
			schema["maxLength"] = item.maxLength
		}
		if len(item.enum) > 0 {
			schema["enum"] = item.enum
		}
	case FieldInt:
		schema["type"] = "integer"
		if item.minimum != nil {
			schema["minimum"] = *item.minimum
		}
		if item.maximum != nil {
			schema["maximum"] = *item.maximum
		}
	case FieldBool:
		schema["type"] = "boolean"
	case FieldTime:
		schema["type"] = "string"
		schema["anyOf"] = []any{map[string]any{"format": "date-time"}, map[string]any{"format": "date"}}
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
		if item.typ == FieldInt {
			if number, err := strconv.Atoi(item.defaultRaw); err == nil {
				schema["default"] = number
			}
		} else {
			schema["default"] = item.defaultRaw
		}
	}
	if item.doc != "" {
		schema["description"] = item.doc
	}
	if item.deprecated {
		schema["deprecated"] = true
		if item.replacement != "" {
			schema["description"] = strings.TrimSpace(item.doc + " Deprecated: use " + item.replacement + ".")
		}
	}
	return schema
}

func alternation(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = regexp.QuoteMeta(value)
	}
	return strings.Join(quoted, "|")
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
