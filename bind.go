package apicontract

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"
)

const dayLayout = "2006-01-02"

// BindQuery parses the request's query string into dst, a pointer to a struct
// whose fields carry query tags. It returns a *Problem with status 400 when a
// value cannot be parsed and 422 when a parsed value breaks a rule, including
// a parameter the struct does not declare. Any other error means dst is not a
// valid parameter struct.
func BindQuery(r *http.Request, dst any) error {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return NewProblem(http.StatusBadRequest, CodeMalformedQuery, "The query string is not valid URL encoding.")
	}
	return BindValues(values, dst)
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

	problems := problemSet{in: InQuery}
	for _, name := range sortedKeys(values) {
		if p.index(name) < 0 {
			problems.add(false, FieldError{In: InQuery, Name: name, Code: FieldUnknown, Detail: "is not a parameter of this route"})
		}
	}

	raw := make([]string, len(p.params))
	used := make([]string, len(p.params))
	present := make([]bool, len(p.params))
	for i, item := range p.params {
		if len(values[item.name]) > 1 {
			problems.add(true, FieldError{In: InQuery, Name: item.name, Code: FieldInvalid, Detail: "must be sent once"})
			continue
		}
		if value := strings.TrimSpace(values.Get(item.name)); value != "" {
			raw[i], used[i], present[i] = value, item.name, true
		}
	}

	for i, item := range p.params {
		if item.replacement == "" || !present[i] {
			continue
		}
		j := p.index(item.replacement)
		switch {
		case !present[j]:
			raw[j], used[j], present[j] = raw[i], item.name, true
		case raw[j] != raw[i]:
			problems.add(false, FieldError{In: InQuery, Name: item.name, Code: FieldConflict, Detail: fmt.Sprintf("%s and %s disagree; send only %s", item.name, item.replacement, item.replacement)})
		}
	}

	location := time.UTC
	for i, item := range p.params {
		if item.typ != FieldLocation {
			continue
		}
		name, value := used[i], raw[i]
		if !present[i] {
			if item.required {
				problems.add(false, FieldError{In: InQuery, Name: item.name, Code: FieldRequired, Detail: "is required"})
				continue
			}
			if item.defaultRaw == "" {
				continue
			}
			name, value = item.name, item.defaultRaw
		}
		loaded, err := loadLocation(value)
		if err != nil {
			problems.add(true, FieldError{In: InQuery, Name: name, Code: FieldInvalid, Detail: "must be an IANA time zone such as Europe/Berlin"})
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
				problems.add(false, FieldError{In: InQuery, Name: item.name, Code: FieldRequired, Detail: "is required"})
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
			problems.add(false, FieldError{In: InQuery, Name: name, Code: FieldOutOfRange, Detail: fmt.Sprintf("must be after %s", p.params[group.from].name)})
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

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func loadLocation(name string) (*time.Location, error) {
	if name == "Local" {
		return nil, fmt.Errorf("local time zone is not allowed")
	}
	return time.LoadLocation(name)
}

func (item param) set(field reflect.Value, name, value string, location *time.Location) (*FieldError, bool) {
	invalid := func(detail string) (*FieldError, bool) {
		return &FieldError{In: InQuery, Name: name, Code: FieldInvalid, Detail: detail}, true
	}
	rule := func(code, detail string) (*FieldError, bool) {
		return &FieldError{In: InQuery, Name: name, Code: code, Detail: detail}, false
	}
	switch item.typ {
	case FieldString:
		if code, detail := item.checkString(value); code != "" {
			return rule(code, detail)
		}
		field.SetString(value)
	case FieldInt:
		number, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return invalid("must be an integer")
		}
		number, code, detail := item.checkInt(number, field)
		if code != "" {
			return rule(code, detail)
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
	case FieldDate:
		day, err := ParseDate(value)
		if err != nil {
			return invalid("must be a YYYY-MM-DD day")
		}
		field.Set(reflect.ValueOf(day))
	case FieldStrings:
		parts := splitList(value)
		if parts == nil {
			return invalid("must be a comma-separated list")
		}
		if item.maxItems > 0 && len(parts) > item.maxItems {
			return rule(FieldTooMany, fmt.Sprintf("must have at most %d values", item.maxItems))
		}
		for _, part := range parts {
			if code, detail := item.checkString(part); code != "" {
				return rule(code, "each value "+detail)
			}
		}
		field.Set(reflect.ValueOf(parts))
	case FieldSort:
		parts := splitList(value)
		if parts == nil {
			return invalid("must be a comma-separated list of fields, each optionally prefixed with -")
		}
		if item.maxItems > 0 && len(parts) > item.maxItems {
			return rule(FieldTooMany, fmt.Sprintf("must have at most %d fields", item.maxItems))
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

func (r rules) checkString(value string) (string, string) {
	if r.maxLength > 0 && utf8.RuneCountInString(value) > r.maxLength {
		return FieldTooLong, fmt.Sprintf("must be at most %d characters", r.maxLength)
	}
	if len(r.enum) > 0 && !contains(r.enum, value) {
		return FieldUnknownValue, "must be one of " + strings.Join(r.enum, ", ")
	}
	return "", ""
}

func (r rules) checkInt(number int64, field reflect.Value) (int64, string, string) {
	below := r.minimum != nil && float64(number) < *r.minimum
	if r.minInt != nil {
		below = number < *r.minInt
	}
	if below {
		return 0, FieldOutOfRange, "must be at least " + formatNumber(*r.minimum)
	}
	above := r.maximum != nil && float64(number) > *r.maximum
	if r.maxInt != nil {
		above = number > *r.maxInt
	}
	if above {
		if !r.clamp {
			return 0, FieldOutOfRange, "must be at most " + formatNumber(*r.maximum)
		}
		number = int64(math.Floor(*r.maximum))
		if r.maxInt != nil {
			number = *r.maxInt
		}
	}
	if field.OverflowInt(number) {
		return 0, FieldOutOfRange, "is too large"
	}
	return number, "", ""
}

func (r rules) checkNumber(number float64, field reflect.Value) (float64, string, string) {
	if r.minimum != nil && number < *r.minimum {
		return 0, FieldOutOfRange, "must be at least " + formatNumber(*r.minimum)
	}
	if r.maximum != nil && number > *r.maximum {
		if !r.clamp {
			return 0, FieldOutOfRange, "must be at most " + formatNumber(*r.maximum)
		}
		number = *r.maximum
	}
	if field.OverflowFloat(number) {
		return 0, FieldOutOfRange, "is too large"
	}
	return number, "", ""
}

func formatNumber(number float64) string {
	return strconv.FormatFloat(number, 'f', -1, 64)
}

var timestampPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-](\d{2}):(\d{2}))$`)

// parseTimestamp accepts only RFC 3339 date-time values, which time.Parse
// alone does not guarantee: it also takes one-digit hours, comma fractions and
// offsets of 24 hours or more.
func parseTimestamp(value string) (time.Time, error) {
	match := timestampPattern.FindStringSubmatch(value)
	if match == nil {
		return time.Time{}, errors.New("not an RFC 3339 timestamp")
	}
	if match[3] != "" && (match[3] > "23" || match[4] > "59") {
		return time.Time{}, errors.New("offset out of range")
	}
	return time.Parse(time.RFC3339Nano, value)
}

func parseTime(value string, location *time.Location, end bool) (time.Time, error) {
	if moment, err := parseTimestamp(value); err == nil {
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
	in        string
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
	codes := map[string][2]string{
		InQuery: {CodeMalformedQuery, CodeInvalidQuery},
		InBody:  {CodeMalformedBody, CodeInvalidBody},
	}[s.in]
	if s.malformed {
		return NewProblem(http.StatusBadRequest, codes[0], "", s.fields...)
	}
	return NewProblem(http.StatusUnprocessableEntity, codes[1], "", s.fields...)
}
