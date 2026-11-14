package apicontract

import (
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

const dayLayout = "2006-01-02"

// BindQuery parses the request's query string into dst, a pointer to a struct
// whose fields carry query tags. It returns a *Problem with status 400 when a
// value cannot be parsed and 422 when a parsed value breaks a rule. Any other
// error means dst is not a valid parameter struct.
func BindQuery(r *http.Request, dst any) error {
	return BindValues(r.URL.Query(), dst)
}

// BindValues parses values into dst. See BindQuery.
func BindValues(values url.Values, dst any) error {
	target := reflect.ValueOf(dst)
	if target.Kind() != reflect.Pointer || target.IsNil() {
		return fmt.Errorf("apicontract: destination must be a non-nil pointer to a struct")
	}
	target = target.Elem()
	p, err := planFor(target.Type())
	if err != nil {
		return err
	}

	raw := make([]string, len(p.params))
	used := make([]string, len(p.params))
	present := make([]bool, len(p.params))
	for i, item := range p.params {
		if value := strings.TrimSpace(values.Get(item.name)); value != "" {
			raw[i], used[i], present[i] = value, item.name, true
		}
	}

	var problems problemSet
	for i, item := range p.params {
		if item.replacement == "" || !present[i] {
			continue
		}
		j := p.index(item.replacement)
		switch {
		case !present[j]:
			raw[j], used[j], present[j] = raw[i], item.name, true
		case raw[j] != raw[i]:
			problems.add(false, FieldError{Name: item.name, Code: FieldConflict, Message: fmt.Sprintf("%s and %s disagree; send only %s", item.name, item.replacement, item.replacement)})
		}
	}

	location := time.UTC
	for i, item := range p.params {
		if item.typ != FieldLocation || !present[i] {
			continue
		}
		loaded, err := loadLocation(raw[i])
		if err != nil {
			problems.add(true, FieldError{Name: used[i], Code: FieldInvalid, Message: "must be an IANA time zone such as Europe/Berlin"})
			continue
		}
		location = loaded
		target.FieldByIndex(item.index).Set(reflect.ValueOf(loaded))
	}

	for i, item := range p.params {
		if item.typ == FieldLocation {
			continue
		}
		name, value := used[i], raw[i]
		if !present[i] {
			if item.required {
				problems.add(false, FieldError{Name: item.name, Code: FieldRequired, Message: "is required"})
				continue
			}
			if item.defaultRaw == "" {
				continue
			}
			name, value = item.name, item.defaultRaw
		}
		if fieldErr, malformed := item.set(target.FieldByIndex(item.index), name, value, location); fieldErr != nil {
			problems.add(malformed, *fieldErr)
		}
	}

	for _, group := range p.ranges {
		from := target.FieldByIndex(p.params[group.from].index).Interface().(time.Time)
		to := target.FieldByIndex(p.params[group.to].index).Interface().(time.Time)
		if !from.IsZero() && !to.IsZero() && !to.After(from) {
			name := used[group.to]
			if name == "" {
				name = p.params[group.to].name
			}
			problems.add(false, FieldError{Name: name, Code: FieldOutOfRange, Message: fmt.Sprintf("must be after %s", p.params[group.from].name)})
		}
	}
	return problems.err()
}

func (p *plan) index(name string) int {
	for i, item := range p.params {
		if item.name == name {
			return i
		}
	}
	return -1
}

func loadLocation(name string) (*time.Location, error) {
	if name == "Local" {
		return nil, fmt.Errorf("local time zone is not allowed")
	}
	return time.LoadLocation(name)
}

func (item param) set(field reflect.Value, name, value string, location *time.Location) (*FieldError, bool) {
	invalid := func(message string) (*FieldError, bool) {
		return &FieldError{Name: name, Code: FieldInvalid, Message: message}, true
	}
	rule := func(code, message string) (*FieldError, bool) {
		return &FieldError{Name: name, Code: code, Message: message}, false
	}
	switch item.typ {
	case FieldString:
		if item.maxLength > 0 && len(value) > item.maxLength {
			return rule(FieldTooLong, fmt.Sprintf("must be at most %d bytes", item.maxLength))
		}
		if len(item.enum) > 0 && !contains(item.enum, value) {
			return rule(FieldUnknownValue, "must be one of "+strings.Join(item.enum, ", "))
		}
		field.SetString(value)
	case FieldInt:
		number, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return invalid("must be an integer")
		}
		if item.minimum != nil && number < int64(*item.minimum) {
			return rule(FieldOutOfRange, fmt.Sprintf("must be at least %d", *item.minimum))
		}
		if item.maximum != nil && number > int64(*item.maximum) {
			if !item.clamp {
				return rule(FieldOutOfRange, fmt.Sprintf("must be at most %d", *item.maximum))
			}
			number = int64(*item.maximum)
		}
		if field.OverflowInt(number) {
			return rule(FieldOutOfRange, "is too large")
		}
		field.SetInt(number)
	case FieldBool:
		flag, err := strconv.ParseBool(value)
		if err != nil {
			return invalid("must be true or false")
		}
		field.SetBool(flag)
	case FieldTime:
		moment, err := parseTime(value, location, item.end)
		if err != nil {
			return invalid("must be an RFC 3339 timestamp with offset or a YYYY-MM-DD day")
		}
		field.Set(reflect.ValueOf(moment))
	case FieldStrings:
		parts := splitList(value)
		if parts == nil {
			return invalid("must be a comma-separated list")
		}
		for _, part := range parts {
			if len(item.enum) > 0 && !contains(item.enum, part) {
				return rule(FieldUnknownValue, "values must be among "+strings.Join(item.enum, ", "))
			}
		}
		field.Set(reflect.ValueOf(parts))
	case FieldSort:
		parts := splitList(value)
		if parts == nil {
			return invalid("must be a comma-separated list of fields, each optionally prefixed with -")
		}
		keys := make([]SortKey, 0, len(parts))
		for _, part := range parts {
			key := SortKey{Field: strings.TrimPrefix(part, "-"), Desc: strings.HasPrefix(part, "-")}
			if key.Field == "" {
				return invalid("must be a comma-separated list of fields, each optionally prefixed with -")
			}
			if len(item.enum) > 0 && !contains(item.enum, key.Field) {
				return rule(FieldUnknownValue, "fields must be among "+strings.Join(item.enum, ", "))
			}
			keys = append(keys, key)
		}
		field.Set(reflect.ValueOf(keys))
	}
	return nil, false
}

func parseTime(value string, location *time.Location, end bool) (time.Time, error) {
	if moment, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return moment, nil
	}
	day, err := time.ParseInLocation(dayLayout, value, location)
	if err != nil {
		return time.Time{}, err
	}
	if end {
		day = day.AddDate(0, 0, 1)
	}
	return day, nil
}

func splitList(value string) []string {
	var parts []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil
		}
		parts = append(parts, part)
	}
	return parts
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

type problemSet struct {
	fields    []FieldError
	malformed bool
}

func (s *problemSet) add(malformed bool, field FieldError) {
	s.fields = append(s.fields, field)
	s.malformed = s.malformed || malformed
}

func (s *problemSet) err() error {
	if len(s.fields) == 0 {
		return nil
	}
	if s.malformed {
		return &Problem{Status: http.StatusBadRequest, Code: CodeMalformedQuery, Message: "The query string could not be parsed.", Fields: s.fields}
	}
	return &Problem{Status: http.StatusUnprocessableEntity, Code: CodeInvalidQuery, Message: "The query string is not valid.", Fields: s.fields}
}
