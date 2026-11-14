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

type paramType int

const (
	typeString paramType = iota
	typeInt
	typeBool
	typeTime
	typeLocation
	typeSort
	typeStrings
)

var (
	timeType     = reflect.TypeOf(time.Time{})
	locationType = reflect.TypeOf((*time.Location)(nil))
	sortType     = reflect.TypeOf([]SortKey(nil))
	stringsType  = reflect.TypeOf([]string(nil))
	rangeType    = reflect.TypeOf(Range{})
)

type param struct {
	index       []int
	name        string
	typ         paramType
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
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("apicontract: %s is not a struct", t)
	}
	p := &plan{}
	if err := p.walk(t, nil, ""); err != nil {
		return nil, err
	}
	if err := p.check(); err != nil {
		return nil, err
	}
	plans.Store(t, p)
	return p, nil
}

func (p *plan) walk(t reflect.Type, index []int, prefix string) error {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		tag, hasTag := field.Tag.Lookup("query")
		if tag == "-" {
			continue
		}
		path := append(append([]int(nil), index...), i)
		if field.Type.Kind() == reflect.Struct && field.Type != timeType {
			nested := prefix
			if hasTag && tag != "" {
				nested = prefix + tag + "_"
			} else if !field.Anonymous {
				continue
			}
			start := len(p.params)
			if err := p.walk(field.Type, path, nested); err != nil {
				return err
			}
			if field.Type == rangeType {
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

func newParam(field reflect.StructField, index []int, name string) (param, error) {
	item := param{index: index, name: name, doc: field.Tag.Get("doc")}
	switch {
	case field.Type == timeType:
		item.typ = typeTime
	case field.Type == locationType:
		item.typ = typeLocation
	case field.Type == sortType:
		item.typ = typeSort
	case field.Type == stringsType:
		item.typ = typeStrings
	case field.Type.Kind() == reflect.String:
		item.typ = typeString
	case field.Type.Kind() == reflect.Bool:
		item.typ = typeBool
	case field.Type.Kind() >= reflect.Int && field.Type.Kind() <= reflect.Int64:
		item.typ = typeInt
	default:
		return item, fmt.Errorf("apicontract: query parameter %s has unsupported type %s", name, field.Type)
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
