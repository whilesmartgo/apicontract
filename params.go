package apicontract

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
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

// FieldType classifies a struct field for parameter and body description.
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
	FieldNumber
	FieldDate
	FieldList
	FieldAny
	FieldObject
)

// Field describes one struct field. Static tools that read Go source, such as
// OpenAPI generators, build Fields from the source types and pass them to
// OpenAPIParametersOf or OpenAPIRequestBodyOf, so they describe a struct
// exactly as BindQuery and BindJSON read it. Fields lists every field of the
// struct in order.
type Field struct {
	Name      string
	Exported  bool
	Anonymous bool
	Type      FieldType
	Tag       reflect.StructTag
	// Pointer is true when the field is a pointer to Type. A pointer body
	// field accepts null.
	Pointer bool
	// Bits is the size of an integer or floating-point type, such as 8 for
	// int8. Zero means 64.
	Bits int
	// Range is true when the field's type is apicontract.Range.
	Range bool
	// Fields holds the nested fields when Type is FieldStruct.
	Fields []Field
	// Elem describes the element type when Type is FieldList.
	Elem *Field
}

var (
	timeType     = reflect.TypeOf(time.Time{})
	dateType     = reflect.TypeOf(Date{})
	locationType = reflect.TypeOf((*time.Location)(nil))
	sortType     = reflect.TypeOf([]SortKey(nil))
	stringsType  = reflect.TypeOf([]string(nil))
	rangeType    = reflect.TypeOf(Range{})
	rawType      = reflect.TypeOf(json.RawMessage(nil))
)

func fieldsOf(t reflect.Type) []Field {
	return fieldsIn(t, map[reflect.Type]bool{})
}

func fieldsIn(t reflect.Type, seen map[reflect.Type]bool) []Field {
	seen[t] = true
	defer delete(seen, t)
	fields := make([]Field, t.NumField())
	for i := range fields {
		field := t.Field(i)
		fields[i] = describe(field.Type, seen)
		fields[i].Name, fields[i].Exported, fields[i].Anonymous, fields[i].Tag = field.Name, field.IsExported(), field.Anonymous, field.Tag
	}
	return fields
}

func describe(t reflect.Type, seen map[reflect.Type]bool) Field {
	if t == locationType {
		return Field{Type: FieldLocation}
	}
	if t.Kind() == reflect.Pointer {
		field := describe(t.Elem(), seen)
		if field.Pointer || field.Type == FieldLocation {
			return Field{Type: FieldOther}
		}
		field.Pointer = true
		return field
	}
	field := Field{Type: fieldType(t)}
	switch field.Type {
	case FieldInt, FieldNumber:
		field.Bits = t.Bits()
	case FieldStruct:
		if seen[t] {
			return Field{Type: FieldOther}
		}
		field.Range = t == rangeType
		field.Fields = fieldsIn(t, seen)
	case FieldList:
		elem := describe(t.Elem(), seen)
		field.Elem = &elem
	}
	return field
}

func fieldType(t reflect.Type) FieldType {
	switch {
	case t == timeType:
		return FieldTime
	case t == dateType:
		return FieldDate
	case t == sortType:
		return FieldSort
	case t == stringsType:
		return FieldStrings
	case t == rawType, t.Kind() == reflect.Interface && t.NumMethod() == 0:
		return FieldAny
	case t.Kind() == reflect.Map && t.Key().Kind() == reflect.String && t.Elem().Kind() == reflect.Interface && t.Elem().NumMethod() == 0:
		return FieldObject
	case t.Kind() == reflect.Struct:
		return FieldStruct
	case t.Kind() == reflect.Slice:
		return FieldList
	case t.Kind() == reflect.String:
		return FieldString
	case t.Kind() == reflect.Bool:
		return FieldBool
	case t.Kind() >= reflect.Int && t.Kind() <= reflect.Int64:
		return FieldInt
	case t.Kind() == reflect.Float32 || t.Kind() == reflect.Float64:
		return FieldNumber
	}
	return FieldOther
}

// rules are the validation tags shared by query parameters and body fields.
type rules struct {
	doc         string
	required    bool
	deprecated  bool
	replacement string
	defaultRaw  string
	minimum     *float64
	maximum     *float64
	minInt      *int64
	maxInt      *int64
	bits        int
	clamp       bool
	maxLength   int
	maxItems    int
	enum        []string
	end         bool
}

func parseRules(tag reflect.StructTag, name string) (rules, error) {
	r := rules{doc: tag.Get("doc"), defaultRaw: tag.Get("default")}
	r.required = tag.Get("required") == "true"
	r.end = tag.Get("bound") == "end"
	r.clamp = tag.Get("clamp") == "true"
	if value, ok := tag.Lookup("deprecated"); ok {
		r.deprecated = true
		if value != "" && value != "-" {
			r.replacement = value
		}
	}
	if value := tag.Get("enum"); value != "" {
		r.enum = strings.Split(value, ",")
	}
	for _, bound := range []struct {
		key     string
		target  **float64
		integer **int64
	}{{"minimum", &r.minimum, &r.minInt}, {"maximum", &r.maximum, &r.maxInt}} {
		value := tag.Get(bound.key)
		if value == "" {
			continue
		}
		number, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
			return r, fmt.Errorf("apicontract: %s of %s is not a number", bound.key, name)
		}
		*bound.target = &number
		if exact, ok := new(big.Rat).SetString(value); ok {
			whole := new(big.Int).Div(exact.Num(), exact.Denom())
			if bound.key == "minimum" && !exact.IsInt() {
				whole.Add(whole, big.NewInt(1))
			}
			if whole.IsInt64() {
				integer := whole.Int64()
				*bound.integer = &integer
			}
		}
	}
	for _, limit := range []struct {
		key    string
		target *int
	}{{"maxLength", &r.maxLength}, {"maxItems", &r.maxItems}} {
		value := tag.Get(limit.key)
		if value == "" {
			continue
		}
		number, err := strconv.Atoi(value)
		if err != nil || number < 1 {
			return r, fmt.Errorf("apicontract: %s of %s is not a positive integer", limit.key, name)
		}
		*limit.target = number
	}
	return r, nil
}

type param struct {
	rules
	index []int
	name  string
	typ   FieldType
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
			if field.Pointer {
				return fmt.Errorf("apicontract: query struct %s must not be a pointer", field.Name)
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
	item := param{index: index, name: name, typ: field.Type}
	switch field.Type {
	case FieldString, FieldInt, FieldBool, FieldTime, FieldDate, FieldLocation, FieldSort, FieldStrings:
	default:
		return item, fmt.Errorf("apicontract: query parameter %s has an unsupported type", name)
	}
	if field.Pointer {
		return item, fmt.Errorf("apicontract: query parameter %s must not be a pointer", name)
	}
	r, err := parseRules(field.Tag, name)
	if err != nil {
		return item, err
	}
	r.bits = field.Bits
	item.rules = r
	if item.defaultRaw != "" {
		if err := item.checkDefault(); err != nil {
			return item, fmt.Errorf("apicontract: default of %s: %w", name, err)
		}
	}
	return item, nil
}

func (item param) checkDefault() error {
	var target reflect.Value
	switch item.typ {
	case FieldString:
		target = reflect.New(reflect.TypeOf("")).Elem()
	case FieldInt:
		target = reflect.New(reflect.TypeOf(int64(0))).Elem()
	case FieldBool:
		target = reflect.New(reflect.TypeOf(false)).Elem()
	case FieldTime:
		target = reflect.New(timeType).Elem()
	case FieldDate:
		target = reflect.New(dateType).Elem()
	case FieldStrings:
		target = reflect.New(stringsType).Elem()
	case FieldSort:
		target = reflect.New(sortType).Elem()
	case FieldLocation:
		_, err := loadLocation(item.defaultRaw)
		return err
	}
	if fieldErr, _ := item.set(target, item.name, item.defaultRaw, time.UTC); fieldErr != nil {
		return fmt.Errorf("%s %s", item.defaultRaw, fieldErr.Detail)
	}
	if item.typ == FieldInt {
		return item.checkIntDefault(target.Int())
	}
	return nil
}

// integerRange is the range of a signed integer of the given size.
func integerRange(bits int) (int64, int64) {
	if bits <= 0 || bits >= 64 {
		return math.MinInt64, math.MaxInt64
	}
	return -1 << (bits - 1), 1<<(bits-1) - 1
}

// checkIntDefault rejects a default the field cannot hold or that a clamp
// would change, since defaults are stored as declared.
func (r rules) checkIntDefault(value int64) error {
	low, high := integerRange(r.bits)
	if value < low || value > high {
		return fmt.Errorf("%d does not fit a %d-bit integer", value, r.bits)
	}
	if r.maxInt != nil && value > *r.maxInt || r.maximum != nil && float64(value) > *r.maximum {
		return fmt.Errorf("%d is above the maximum", value)
	}
	return nil
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
