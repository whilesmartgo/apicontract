package apicontract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"mime"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
)

// DefaultMaxBodyBytes is the request body size BindJSON accepts unless MaxBytes
// says otherwise.
const DefaultMaxBodyBytes = 1 << 20

const maxDepth = 64

// BodyOption configures BindJSON.
type BodyOption func(*bodyConfig)

type bodyConfig struct {
	maxBytes int64
}

// MaxBytes sets the largest body BindJSON and BindJSONBytes accept. It must
// be positive.
func MaxBytes(n int64) BodyOption {
	return func(c *bodyConfig) { c.maxBytes = n }
}

func configure(options []BodyOption) (bodyConfig, error) {
	config := bodyConfig{maxBytes: DefaultMaxBodyBytes}
	for _, option := range options {
		option(&config)
	}
	if config.maxBytes <= 0 || config.maxBytes == math.MaxInt64 {
		return config, fmt.Errorf("apicontract: MaxBytes must be between 1 and %d", int64(math.MaxInt64-1))
	}
	return config, nil
}

func tooLarge(limit int64) error {
	return NewProblem(http.StatusRequestEntityTooLarge, CodeBodyTooLarge, fmt.Sprintf("The request body must be at most %d bytes.", limit))
}

// BindJSON reads the request's JSON body into dst, a pointer to a struct whose
// fields carry json tags and the same rule tags as query structs. It returns a
// *Problem with status 415 when the body is not JSON, 413 when it is larger
// than the limit, 400 when it cannot be parsed or a value has the wrong type,
// and 422 when a parsed value breaks a rule, including a field the struct does
// not declare. Any other error means dst is not a valid body struct.
func BindJSON(r *http.Request, dst any, options ...BodyOption) error {
	config, err := configure(options)
	if err != nil {
		return err
	}
	if _, err := bodyPlanFor(dst); err != nil {
		return err
	}
	if !isJSONMediaType(r.Header.Get("Content-Type")) {
		return NewProblem(http.StatusUnsupportedMediaType, CodeUnsupportedMediaType, "Send the body as application/json.")
	}
	if r.Body == nil {
		return NewProblem(http.StatusBadRequest, CodeMalformedBody, "The request body is empty.")
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, config.maxBytes+1))
	if err != nil {
		return NewProblem(http.StatusBadRequest, CodeMalformedBody, "The request body could not be read.")
	}
	if int64(len(data)) > config.maxBytes {
		return tooLarge(config.maxBytes)
	}
	return BindJSONBytes(data, dst, options...)
}

// BindJSONBytes parses a JSON object, such as a request body or a tool call's
// arguments, into dst, with the same size limit and rules as BindJSON.
func BindJSONBytes(data []byte, dst any, options ...BodyOption) error {
	config, err := configure(options)
	if err != nil {
		return err
	}
	p, err := bodyPlanFor(dst)
	if err != nil {
		return err
	}
	if int64(len(data)) > config.maxBytes {
		return tooLarge(config.maxBytes)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return NewProblem(http.StatusBadRequest, CodeMalformedBody, "The request body is empty.")
	}
	value, err := parseJSON(data)
	if err != nil {
		return NewProblem(http.StatusBadRequest, CodeMalformedBody, "The request body is not valid JSON: "+err.Error()+".")
	}
	object, ok := value.(map[string]any)
	if !ok {
		return NewProblem(http.StatusBadRequest, CodeMalformedBody, "The request body must be a JSON object.")
	}
	problems := problemSet{in: InBody}
	p.bind(reflect.ValueOf(dst).Elem(), object, "", &problems)
	return problems.err()
}

func isJSONMediaType(header string) bool {
	media, params, err := mime.ParseMediaType(header)
	if err != nil {
		return false
	}
	if charset, ok := params["charset"]; ok && !strings.EqualFold(charset, "utf-8") {
		return false
	}
	return media == "application/json" || (strings.HasPrefix(media, "application/") && strings.HasSuffix(media, "+json"))
}

func parseJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := readValue(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after the JSON value")
	}
	return value, nil
}

func readValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("nested deeper than %d levels", maxDepth)
	}
	token, err := decoder.Token()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("unexpected end of input")
		}
		return nil, err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return token, nil
	}
	switch delim {
	case '{':
		object := map[string]any{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			name := key.(string)
			if _, dup := object[name]; dup {
				return nil, fmt.Errorf("duplicate key %q", name)
			}
			value, err := readValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			object[name] = value
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return object, nil
	case '[':
		list := []any{}
		for decoder.More() {
			value, err := readValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			list = append(list, value)
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
		return list, nil
	}
	return nil, fmt.Errorf("unexpected %v", delim)
}

type bodyField struct {
	rules
	index   []int
	name    string
	typ     FieldType
	pointer bool
	nested  *bodyPlan
	elem    *bodyField
}

type bodyPlan struct {
	fields []bodyField
	names  map[string]int
}

var bodyPlans sync.Map

func bodyPlanFor(dst any) (*bodyPlan, error) {
	target := reflect.ValueOf(dst)
	if target.Kind() != reflect.Pointer || target.IsNil() || target.Elem().Kind() != reflect.Struct {
		return nil, errors.New("apicontract: destination must be a non-nil pointer to a struct")
	}
	t := target.Elem().Type()
	if cached, ok := bodyPlans.Load(t); ok {
		return cached.(*bodyPlan), nil
	}
	p, err := bodyPlanOf(fieldsOf(t), "")
	if err != nil {
		return nil, err
	}
	bodyPlans.Store(t, p)
	return p, nil
}

func bodyPlanOf(fields []Field, path string) (*bodyPlan, error) {
	p := &bodyPlan{names: map[string]int{}}
	if err := p.walk(fields, nil, path); err != nil {
		return nil, err
	}
	for _, field := range p.fields {
		if field.replacement == "" {
			continue
		}
		target, ok := p.names[field.replacement]
		if !ok {
			return nil, fmt.Errorf("apicontract: %s is deprecated for %s, which is not declared", joinPath(path, field.name), field.replacement)
		}
		if p.fields[target].typ != field.typ || p.fields[target].pointer != field.pointer {
			return nil, fmt.Errorf("apicontract: %s and its replacement %s have different types", joinPath(path, field.name), field.replacement)
		}
	}
	return p, nil
}

func jsonName(field Field) (string, bool) {
	tag, ok := field.Tag.Lookup("json")
	if tag == "-" {
		return "", false
	}
	name, _, _ := strings.Cut(tag, ",")
	if !ok || name == "" {
		return field.Name, true
	}
	return name, true
}

func (p *bodyPlan) walk(fields []Field, index []int, path string) error {
	for i, field := range fields {
		name, ok := jsonName(field)
		if !ok {
			continue
		}
		position := append(append([]int(nil), index...), i)
		_, tagged := field.Tag.Lookup("json")
		if field.Anonymous && !tagged && field.Type == FieldStruct && !field.Pointer {
			if err := p.walk(field.Fields, position, path); err != nil {
				return err
			}
			continue
		}
		if !field.Exported {
			continue
		}
		item, err := newBodyField(field, position, name, joinPath(path, name))
		if err != nil {
			return err
		}
		if _, dup := p.names[name]; dup {
			return fmt.Errorf("apicontract: body field %s is declared twice", joinPath(path, name))
		}
		p.names[name] = len(p.fields)
		p.fields = append(p.fields, item)
	}
	return nil
}

func newBodyField(field Field, index []int, name, path string) (bodyField, error) {
	item := bodyField{index: index, name: name, typ: field.Type, pointer: field.Pointer}
	r, err := parseRules(field.Tag, path)
	if err != nil {
		return item, err
	}
	r.bits = field.Bits
	item.rules = r
	if err := item.describe(field, path); err != nil {
		return item, err
	}
	if item.defaultRaw != "" {
		if err := item.checkDefault(); err != nil {
			return item, fmt.Errorf("apicontract: default of %s: %w", path, err)
		}
	}
	return item, nil
}

func (item *bodyField) describe(field Field, path string) error {
	switch field.Type {
	case FieldString, FieldInt, FieldNumber, FieldBool, FieldTime, FieldDate, FieldStrings, FieldAny, FieldObject:
	case FieldStruct:
		nested, err := bodyPlanOf(field.Fields, path)
		if err != nil {
			return err
		}
		item.nested = nested
	case FieldList:
		if field.Elem == nil || field.Elem.Pointer {
			return fmt.Errorf("apicontract: body field %s has an unsupported element type", path)
		}
		elem := bodyField{typ: field.Elem.Type}
		elem.bits = field.Elem.Bits
		if elem.typ == FieldString {
			elem.maxLength, elem.enum = item.maxLength, item.enum
		}
		if err := elem.describe(*field.Elem, path+"[]"); err != nil {
			return err
		}
		item.elem = &elem
	default:
		return fmt.Errorf("apicontract: body field %s has an unsupported type", path)
	}
	return nil
}

func (item bodyField) checkDefault() error {
	value, err := item.parseDefault()
	if err != nil {
		return err
	}
	var code, detail string
	switch typed := value.(type) {
	case string:
		code, detail = item.checkString(typed)
	case int64:
		if err := item.checkIntDefault(typed); err != nil {
			return err
		}
		_, code, detail = item.checkInt(typed, reflect.New(reflect.TypeOf(int64(0))).Elem())
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return errors.New("must be a finite number")
		}
		if item.bits == 32 && math.Abs(typed) > math.MaxFloat32 {
			return fmt.Errorf("%v does not fit a 32-bit float", typed)
		}
		if item.maximum != nil && typed > *item.maximum {
			return fmt.Errorf("%v is above the maximum", typed)
		}
		_, code, detail = item.checkNumber(typed, reflect.New(reflect.TypeOf(float64(0))).Elem())
	}
	if code != "" {
		return errors.New(detail)
	}
	return nil
}

func (item bodyField) parseDefault() (any, error) {
	switch item.typ {
	case FieldString:
		return item.defaultRaw, nil
	case FieldInt:
		return strconv.ParseInt(item.defaultRaw, 10, 64)
	case FieldNumber:
		return strconv.ParseFloat(item.defaultRaw, 64)
	case FieldBool:
		return strconv.ParseBool(item.defaultRaw)
	}
	return nil, errors.New("only string, integer, number and boolean fields take a default")
}

// nullable reports whether an optional field accepts null, which clears it.
func (item bodyField) nullable() bool {
	return item.pointer || item.typ == FieldAny || item.typ == FieldObject
}

func joinPath(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func (p *bodyPlan) bind(target reflect.Value, object map[string]any, path string, problems *problemSet) {
	for _, key := range sortedKeys(object) {
		if _, ok := p.names[key]; !ok {
			problems.add(false, FieldError{In: InBody, Name: joinPath(path, key), Code: FieldUnknown, Detail: "is not a field of this request"})
		}
	}
	used := map[string]string{}
	for _, field := range p.fields {
		value, ok := object[field.name]
		if field.replacement == "" || !ok || value == nil {
			continue
		}
		current, present := object[field.replacement]
		switch {
		case !present || current == nil:
			object[field.replacement], used[field.replacement] = value, field.name
		case !reflect.DeepEqual(current, value):
			problems.add(false, FieldError{In: InBody, Name: joinPath(path, field.name), Code: FieldConflict, Detail: fmt.Sprintf("%s and %s disagree; send only %s", field.name, field.replacement, field.replacement)})
		}
	}
	for _, field := range p.fields {
		name := field.name
		if alias, ok := used[name]; ok {
			name = alias
		}
		full := joinPath(path, name)
		value, present := object[field.name]
		destination := target.FieldByIndex(field.index)
		if !present || value == nil {
			switch {
			case field.required:
				problems.add(false, FieldError{In: InBody, Name: full, Code: FieldRequired, Detail: "is required"})
			case present && !field.nullable():
				problems.add(true, FieldError{In: InBody, Name: full, Code: FieldInvalid, Detail: "must not be null"})
			case present:
				destination.Set(reflect.Zero(destination.Type()))
			case field.defaultRaw != "":
				parsed, _ := field.parseDefault()
				setScalar(destination, field.pointer, parsed)
			}
			continue
		}
		if field.pointer {
			allocated := reflect.New(destination.Type().Elem())
			if field.set(allocated.Elem(), value, full, problems) {
				destination.Set(allocated)
			}
			continue
		}
		field.set(destination, value, full, problems)
	}
}

func setScalar(destination reflect.Value, pointer bool, value any) {
	if pointer {
		allocated := reflect.New(destination.Type().Elem())
		destination.Set(allocated)
		destination = allocated.Elem()
	}
	switch typed := value.(type) {
	case string:
		destination.SetString(typed)
	case int64:
		destination.SetInt(typed)
	case float64:
		destination.SetFloat(typed)
	case bool:
		destination.SetBool(typed)
	}
}

func (item bodyField) set(destination reflect.Value, value any, name string, problems *problemSet) bool {
	invalid := func(detail string) bool {
		problems.add(true, FieldError{In: InBody, Name: name, Code: FieldInvalid, Detail: detail})
		return false
	}
	rule := func(code, detail string) bool {
		problems.add(false, FieldError{In: InBody, Name: name, Code: code, Detail: detail})
		return false
	}
	switch item.typ {
	case FieldString:
		text, ok := value.(string)
		if !ok {
			return invalid("must be a string")
		}
		if code, detail := item.checkString(text); code != "" {
			return rule(code, detail)
		}
		destination.SetString(text)
	case FieldInt:
		number, ok := value.(json.Number)
		if !ok {
			return invalid("must be an integer")
		}
		parsed, err := integer(number)
		if err != nil {
			return invalid(err.Error())
		}
		parsed, code, detail := item.checkInt(parsed, destination)
		if code != "" {
			return rule(code, detail)
		}
		destination.SetInt(parsed)
	case FieldNumber:
		number, ok := value.(json.Number)
		if !ok {
			return invalid("must be a number")
		}
		parsed, err := number.Float64()
		if err != nil {
			return invalid("must be a number")
		}
		parsed, code, detail := item.checkNumber(parsed, destination)
		if code != "" {
			return rule(code, detail)
		}
		destination.SetFloat(parsed)
	case FieldBool:
		flag, ok := value.(bool)
		if !ok {
			return invalid("must be true or false")
		}
		destination.SetBool(flag)
	case FieldTime:
		text, _ := value.(string)
		moment, err := parseTimestamp(text)
		if err != nil {
			return invalid("must be an RFC 3339 timestamp with offset, such as 2026-11-14T09:00:00+01:00")
		}
		destination.Set(reflect.ValueOf(moment))
	case FieldDate:
		text, _ := value.(string)
		day, err := ParseDate(text)
		if err != nil {
			return invalid("must be a YYYY-MM-DD day")
		}
		destination.Set(reflect.ValueOf(day))
	case FieldStrings:
		list, ok := value.([]any)
		if !ok {
			return invalid("must be an array of strings")
		}
		if item.maxItems > 0 && len(list) > item.maxItems {
			return rule(FieldTooMany, fmt.Sprintf("must have at most %d items", item.maxItems))
		}
		texts := make([]string, len(list))
		valid := true
		for i, entry := range list {
			text, ok := entry.(string)
			entryName := fmt.Sprintf("%s[%d]", name, i)
			if !ok {
				problems.add(true, FieldError{In: InBody, Name: entryName, Code: FieldInvalid, Detail: "must be a string"})
				valid = false
				continue
			}
			if code, detail := item.checkString(text); code != "" {
				problems.add(false, FieldError{In: InBody, Name: entryName, Code: code, Detail: detail})
				valid = false
				continue
			}
			texts[i] = text
		}
		if !valid {
			return false
		}
		destination.Set(reflect.ValueOf(texts))
	case FieldList:
		list, ok := value.([]any)
		if !ok {
			return invalid("must be an array")
		}
		if item.maxItems > 0 && len(list) > item.maxItems {
			return rule(FieldTooMany, fmt.Sprintf("must have at most %d items", item.maxItems))
		}
		slice := reflect.MakeSlice(destination.Type(), len(list), len(list))
		valid := true
		for i, entry := range list {
			entryName := fmt.Sprintf("%s[%d]", name, i)
			if entry == nil && item.elem.nullable() {
				continue
			}
			if entry == nil {
				problems.add(true, FieldError{In: InBody, Name: entryName, Code: FieldInvalid, Detail: "must not be null"})
				valid = false
				continue
			}
			if !item.elem.set(slice.Index(i), entry, entryName, problems) {
				valid = false
			}
		}
		if !valid {
			return false
		}
		destination.Set(slice)
	case FieldStruct:
		object, ok := value.(map[string]any)
		if !ok {
			return invalid("must be an object")
		}
		before := len(problems.fields)
		item.nested.bind(destination, object, name, problems)
		return len(problems.fields) == before
	case FieldObject:
		if _, ok := value.(map[string]any); !ok {
			return invalid("must be an object")
		}
		return assignJSON(destination, value, invalid)
	case FieldAny:
		return assignJSON(destination, value, invalid)
	}
	return true
}

// assignJSON stores a free-form value, keeping numbers as json.Number so large
// integers are not rounded through float64.
func assignJSON(destination reflect.Value, value any, invalid func(string) bool) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return invalid("has the wrong type")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(destination.Addr().Interface()); err != nil {
		return invalid("has the wrong type")
	}
	return true
}

// integer accepts any JSON number with no fractional part, such as 1, 1.0 or
// 1e2, as JSON Schema's integer type does.
func integer(number json.Number) (int64, error) {
	if parsed, err := strconv.ParseInt(number.String(), 10, 64); err == nil {
		return parsed, nil
	}
	text := number.String()
	if index := strings.IndexAny(text, "eE"); index >= 0 && len(strings.TrimLeft(text[index+1:], "+-")) > 3 {
		return 0, errors.New("must be an integer")
	}
	exact, ok := new(big.Rat).SetString(text)
	if !ok || !exact.IsInt() {
		return 0, errors.New("must be an integer")
	}
	if !exact.Num().IsInt64() {
		return 0, errors.New("is too large")
	}
	return exact.Num().Int64(), nil
}
