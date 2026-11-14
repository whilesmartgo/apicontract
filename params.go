package apicontract

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Page holds cursor paging parameters. Limit defaults to DefaultLimit and is
// clamped to MaxLimit.
type Page struct {
	Limit  int    `query:"limit" default:"50" minimum:"1" maximum:"200" clamp:"true" doc:"Maximum number of items to return. Values above 200 return 200."`
	Cursor string `query:"cursor" maxLength:"2048" doc:"Opaque cursor from next_cursor of the previous page. Valid only for the same query and the same caller."`
}

// Range is a half-open time range [From, To). A day value (YYYY-MM-DD) in From
// means the start of that day; in To it means the end of that day. Days are
// resolved in the request Zone, or UTC when the struct has no Zone.
//
// Embedded without a query tag the parameters are from and to. As a named
// field with query:"updated" they are updated_from and updated_to.
type Range struct {
	From time.Time `query:"from" doc:"Start of the range, inclusive. RFC 3339 timestamp with offset, or a YYYY-MM-DD day meaning the start of that day."`
	To   time.Time `query:"to" bound:"end" doc:"End of the range. A timestamp is exclusive. A YYYY-MM-DD day includes that whole day."`
}

// Zone holds the IANA time zone used to resolve day values.
type Zone struct {
	Location *time.Location `query:"tz" doc:"IANA time zone used to resolve YYYY-MM-DD days. Defaults to UTC."`
}

// Search holds the free-text search parameter.
type Search struct {
	Q string `query:"q" maxLength:"500" doc:"Free-text search."`
}

// SortKey is one entry of a sort parameter such as sort=created_at,-title.
type SortKey struct {
	Field string
	Desc  bool
}

// FieldType classifies a struct field for parameter description.
type FieldType int

const (
	FieldOther FieldType = iota
	FieldString
	FieldInt
	FieldBool
	FieldTime
	FieldLocation
	FieldSort
	FieldStrings
	FieldStruct
)

// Field describes one struct field. Static tools that read Go source, such as
// OpenAPI generators, build Fields from the source types and pass them to
// OpenAPIParametersOf, so they describe a parameter struct exactly as
// BindQuery reads it. Fields lists every field of the struct in order.
type Field struct {
	Name      string
	Exported  bool
	Anonymous bool
	Type      FieldType
	Tag       reflect.StructTag
	// Range is true when the field's type is apicontract.Range.
	Range bool
	// Fields holds the nested fields when Type is FieldStruct.
	Fields []Field
}

var (
	timeType     = reflect.TypeOf(time.Time{})
	locationType = reflect.TypeOf((*time.Location)(nil))
	sortType     = reflect.TypeOf([]SortKey(nil))
	stringsType  = reflect.TypeOf([]string(nil))
	rangeType    = reflect.TypeOf(Range{})
)

func fieldsOf(t reflect.Type) []Field {
	fields := make([]Field, t.NumField())
	for i := range fields {
		field := t.Field(i)
		fields[i] = Field{Name: field.Name, Exported: field.IsExported(), Anonymous: field.Anonymous, Tag: field.Tag, Type: fieldType(field.Type)}
		if fields[i].Type == FieldStruct {
			fields[i].Range = field.Type == rangeType
			fields[i].Fields = fieldsOf(field.Type)
		}
	}
	return fields
}

func fieldType(t reflect.Type) FieldType {
	switch {
	case t == timeType:
		return FieldTime
	case t == locationType:
		return FieldLocation
	case t == sortType:
		return FieldSort
	case t == stringsType:
		return FieldStrings
	case t.Kind() == reflect.Struct:
		return FieldStruct
	case t.Kind() == reflect.String:
		return FieldString
	case t.Kind() == reflect.Bool:
		return FieldBool
	case t.Kind() >= reflect.Int && t.Kind() <= reflect.Int64:
		return FieldInt
	}
	return FieldOther
}

type param struct {
	index       []int
	name        string
	typ         FieldType
	doc         string
	required    bool
	deprecated  bool
	replacement string
	defaultRaw  string
	minimum     *int
	maximum     *int
	clamp       bool
	maxLength   int
	enum        []string
	end         bool
}

type rangeGroup struct {
	from, to int
}

type plan struct {
	params []param
	ranges []rangeGroup
}

var plans sync.Map

func planFor(t reflect.Type) (*plan, error) {
	if cached, ok := plans.Load(t); ok {
		return cached.(*plan), nil
	}
	if t == nil || t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("apicontract: %v is not a struct", t)
	}
	p, err := planOf(fieldsOf(t))
	if err != nil {
		return nil, err
	}
	plans.Store(t, p)
	return p, nil
}

func planOf(fields []Field) (*plan, error) {
	p := &plan{}
	if err := p.walk(fields, nil, ""); err != nil {
		return nil, err
	}
	if err := p.check(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *plan) walk(fields []Field, index []int, prefix string) error {
	for i, field := range fields {
		if !field.Exported {
			continue
		}
		tag, hasTag := field.Tag.Lookup("query")
		if tag == "-" {
			continue
		}
		path := append(append([]int(nil), index...), i)
		if field.Type == FieldStruct {
			nested := prefix
			if hasTag && tag != "" {
				nested = prefix + tag + "_"
			} else if !field.Anonymous {
				continue
			}
			start := len(p.params)
			if err := p.walk(field.Fields, path, nested); err != nil {
				return err
			}
			if field.Range {
				p.ranges = append(p.ranges, rangeGroup{from: start, to: start + 1})
			}
			continue
		}
		if !hasTag || tag == "" {
			continue
		}
		item, err := newParam(field, path, prefix+tag)
		if err != nil {
			return err
		}
		p.params = append(p.params, item)
	}
	return nil
}

func newParam(field Field, index []int, name string) (param, error) {
	item := param{index: index, name: name, doc: field.Tag.Get("doc"), typ: field.Type}
	if field.Type == FieldOther {
		return item, fmt.Errorf("apicontract: query parameter %s has an unsupported type", name)
	}
	item.required = field.Tag.Get("required") == "true"
	item.end = field.Tag.Get("bound") == "end"
	item.clamp = field.Tag.Get("clamp") == "true"
	item.defaultRaw = field.Tag.Get("default")
	if value, ok := field.Tag.Lookup("deprecated"); ok {
		item.deprecated = true
		if value != "" && value != "-" {
			item.replacement = value
		}
	}
	if value := field.Tag.Get("enum"); value != "" {
		item.enum = strings.Split(value, ",")
	}
	for _, bound := range []struct {
		key    string
		target **int
	}{{"minimum", &item.minimum}, {"maximum", &item.maximum}} {
		value := field.Tag.Get(bound.key)
		if value == "" {
			continue
		}
		number, err := strconv.Atoi(value)
		if err != nil {
			return item, fmt.Errorf("apicontract: %s of %s is not an integer", bound.key, name)
		}
		*bound.target = &number
	}
	if value := field.Tag.Get("maxLength"); value != "" {
		number, err := strconv.Atoi(value)
		if err != nil {
			return item, fmt.Errorf("apicontract: maxLength of %s is not an integer", name)
		}
		item.maxLength = number
	}
	return item, nil
}

func (p *plan) check() error {
	names := map[string]int{}
	for i, item := range p.params {
		if _, ok := names[item.name]; ok {
			return fmt.Errorf("apicontract: query parameter %s is declared twice", item.name)
		}
		names[item.name] = i
	}
	for _, item := range p.params {
		if item.replacement == "" {
			continue
		}
		target, ok := names[item.replacement]
		if !ok {
			return fmt.Errorf("apicontract: %s is deprecated for %s, which is not declared", item.name, item.replacement)
		}
		if p.params[target].typ != item.typ {
			return fmt.Errorf("apicontract: %s and its replacement %s have different types", item.name, item.replacement)
		}
	}
	return nil
}
