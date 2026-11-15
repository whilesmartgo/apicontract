// Package lint checks an OpenAPI document against the API contract.
package lint

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/whilesmartgo/apicontract"
)

// Rule names reported in Violation.Rule.
const (
	RuleContractVersion = "contract-version"
	RuleParamName       = "param-name"
	RuleParamLegacy     = "param-legacy"
	RuleParamTime       = "param-time-format"
	RuleListEnvelope    = "list-envelope"
	RuleListLimit       = "list-limit"
	RuleListCursor      = "list-cursor"
	RuleErrorResponses  = "error-responses"
	RuleResponseSchema  = "response-schema"
	RuleErrorMediaType  = "error-media-type"
	RuleBodyName        = "body-name"
	RuleBodyTime        = "body-time-format"
	RuleBodyClosed      = "body-closed"
)

// Violation is one place where a document breaks a rule. Subject names the
// parameter involved, when there is one.
type Violation struct {
	Spec    string `json:"spec"`
	Rule    string `json:"rule"`
	Method  string `json:"method"`
	Path    string `json:"path"`
	Subject string `json:"subject,omitempty"`
	Message string `json:"message"`
}

func (v Violation) String() string {
	where := v.Spec
	if v.Method != "" {
		where += ": " + v.Method + " " + v.Path
	}
	return fmt.Sprintf("%s: %s: %s", where, v.Rule, v.Message)
}

var (
	snakeCase  = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	legacy     = map[string]string{"page": "cursor", "per_page": "limit", "page_size": "limit", "offset": "cursor", "start": "from", "end": "to", "query": "q", "search": "q"}
	timeSuffix = []string{"_from", "_to", "_since", "_until"}
	timeNames  = map[string]bool{"from": true, "to": true, "since": true, "until": true}
	methods    = []string{"get", "put", "post", "patch", "delete", "head", "options", "trace"}
	paging     = []string{"limit", "cursor", "page", "per_page", "page_size", "offset"}
)

// Check returns the violations in the OpenAPI document data. spec names the
// document in each violation.
func Check(spec string, data []byte) ([]Violation, error) {
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("%s: %w", spec, err)
	}
	c := checker{spec: spec, document: document}
	c.run()
	if c.work > maxWork {
		return nil, fmt.Errorf("%s: schemas are too large or too deeply composed to check", spec)
	}
	sort.SliceStable(c.found, func(i, j int) bool { return c.found[i].key() < c.found[j].key() })
	return c.found, nil
}

func (v Violation) key() string {
	return strings.Join([]string{v.Spec, v.Path, v.Method, v.Rule, v.Subject}, "\x00")
}

const maxWork = 100000

type checker struct {
	spec     string
	document map[string]any
	found    []Violation
	work     int
}

func (c *checker) report(rule, method, path, subject, message string) {
	c.found = append(c.found, Violation{Spec: c.spec, Rule: rule, Method: method, Path: path, Subject: subject, Message: message})
}

func (c *checker) run() {
	info, _ := c.document["info"].(map[string]any)
	if version, _ := info[apicontract.VersionExtension].(string); version == "" {
		c.report(RuleContractVersion, "", "", "", "info."+apicontract.VersionExtension+" must state the contract version this API meets")
	}
	paths, _ := c.document["paths"].(map[string]any)
	names := make([]string, 0, len(paths))
	for path := range paths {
		names = append(names, path)
	}
	sort.Strings(names)
	for _, path := range names {
		item, _ := c.resolve(paths[path]).(map[string]any)
		for _, method := range methods {
			operation, ok := item[method].(map[string]any)
			if !ok {
				continue
			}
			c.operation(strings.ToUpper(method), path, c.parameters(item, operation), operation)
		}
	}
}

func (c *checker) parameters(item, operation map[string]any) map[string]map[string]any {
	query := map[string]map[string]any{}
	for _, source := range []map[string]any{item, operation} {
		list, _ := source["parameters"].([]any)
		for _, entry := range list {
			parameter, _ := c.resolve(entry).(map[string]any)
			if parameter["in"] != "query" {
				continue
			}
			if name, _ := parameter["name"].(string); name != "" {
				query[name] = parameter
			}
		}
	}
	return query
}

func (c *checker) operation(method, path string, query map[string]map[string]any, operation map[string]any) {
	names := make([]string, 0, len(query))
	for name := range query {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		parameter := query[name]
		deprecated, _ := parameter["deprecated"].(bool)
		if !snakeCase.MatchString(name) {
			c.report(RuleParamName, method, path, name, fmt.Sprintf("query parameter %q must be snake_case", name))
		}
		if replacement, ok := legacy[name]; ok && !deprecated {
			c.report(RuleParamLegacy, method, path, name, fmt.Sprintf("query parameter %q must be deprecated in favour of %q", name, replacement))
		}
		if isTimeName(name) && !hasTimeFormat(c.resolveSchema(parameter["schema"])) {
			c.report(RuleParamTime, method, path, name, fmt.Sprintf("query parameter %q must declare format date or date-time", name))
		}
	}

	responses, _ := operation["responses"].(map[string]any)
	_, hasBody := operation["requestBody"]
	if (len(query) > 0 || hasBody) && responses["400"] == nil && responses["422"] == nil && responses["4XX"] == nil {
		c.report(RuleErrorResponses, method, path, "", "operations with query parameters or a body must declare a 400 or 422 response")
	}
	for _, status := range sortedKeys(responses) {
		if !strings.HasPrefix(status, "4") && !strings.HasPrefix(status, "5") {
			continue
		}
		response, ok := c.resolve(responses[status]).(map[string]any)
		if !ok {
			continue
		}
		content, _ := response["content"].(map[string]any)
		if _, ok := content[apicontract.ProblemContentType]; !ok {
			c.report(RuleErrorMediaType, method, path, status, fmt.Sprintf("the %s response must be %s", status, apicontract.ProblemContentType))
		}
	}
	if body, ok := c.resolve(operation["requestBody"]).(map[string]any); ok {
		content, _ := body["content"].(map[string]any)
		for _, media := range sortedKeys(content) {
			if media != "application/json" && !(strings.HasPrefix(media, "application/") && strings.HasSuffix(media, "+json")) {
				continue
			}
			entry, _ := content[media].(map[string]any)
			c.body(method, path, entry["schema"], "", map[string]bool{}, false)
		}
	}

	if method != "GET" {
		return
	}
	shape := c.listShape(responses)
	if shape == undeclared {
		c.report(RuleResponseSchema, method, path, "", "the 200 response must declare its JSON schema")
	}
	if shape == notList || shape == undeclared {
		if !hasAny(query, paging) {
			return
		}
	}
	if shape != envelope {
		c.report(RuleListEnvelope, method, path, "", `collections must return {"items": [...], "next_cursor": "..."}`)
	}
	limit, ok := query["limit"]
	if !ok {
		c.report(RuleListLimit, method, path, "limit", fmt.Sprintf("collections must accept limit with maximum %d", apicontract.MaxLimit))
	} else {
		schema := c.resolveSchema(limit["schema"])
		maximum, ok := schema["maximum"].(float64)
		if schema["type"] != "integer" || !ok || maximum > apicontract.MaxLimit {
			c.report(RuleListLimit, method, path, "limit", fmt.Sprintf("limit must be an integer with maximum at most %d", apicontract.MaxLimit))
		}
	}
	if _, ok := query["cursor"]; !ok {
		c.report(RuleListCursor, method, path, "cursor", "collections must accept an opaque cursor")
	}
}

// body checks a request schema. closed is true when an enclosing schema that
// applies to the same object already rejects unknown properties, as it does
// for the options of its anyOf and oneOf.
func (c *checker) body(method, path string, value any, at string, seen map[string]bool, closed bool) {
	var entered []string
	parts := c.parts(value, seen, &entered)
	inner := extend(seen, entered)
	properties := map[string]any{}
	other := false
	for _, schema := range parts {
		if schema["additionalProperties"] == false {
			closed = true
		}
	}
	for _, schema := range parts {
		for _, key := range []string{"anyOf", "oneOf"} {
			options, _ := schema[key].([]any)
			for _, option := range options {
				c.body(method, path, option, at, inner, closed)
			}
		}
		if isArray(schema) {
			c.body(method, path, schema["items"], at+"[]", inner, false)
		}
		declared, _ := schema["properties"].(map[string]any)
		for name, property := range declared {
			if _, ok := properties[name]; !ok {
				properties[name] = property
			}
		}
		if _, typed := schema["type"]; typed && !hasType(schema, "object") {
			other = true
		}
	}
	if len(properties) == 0 {
		return
	}
	if !closed && !other {
		subject := at
		if subject == "" {
			subject = "(body)"
		}
		c.report(RuleBodyClosed, method, path, subject, "request objects must set additionalProperties to false so unknown fields are rejected")
	}
	for _, name := range sortedKeys(properties) {
		field := name
		if at != "" {
			field = at + "." + name
		}
		if !snakeCase.MatchString(name) {
			c.report(RuleBodyName, method, path, field, fmt.Sprintf("body field %q must be snake_case", field))
		}
		if strings.HasSuffix(name, "_at") && !c.hasFormat(properties[name], "date-time") {
			c.report(RuleBodyTime, method, path, field, fmt.Sprintf("body field %q must declare format date-time", field))
		}
		c.body(method, path, properties[name], field, inner, false)
	}
}

func extend(seen map[string]bool, refs []string) map[string]bool {
	out := make(map[string]bool, len(seen)+len(refs))
	for ref := range seen {
		out[ref] = true
	}
	for _, ref := range refs {
		out[ref] = true
	}
	return out
}

// parts returns the schemas that all apply to value: the schema itself, the
// target of its $ref, the keywords written beside that $ref, and the members
// of its allOf, followed through references. A reference already in seen is
// skipped, and entered collects the references followed, so a caller walking
// deeper can stop at a schema that contains itself. The walk stops after a
// fixed amount of work, so a very large document cannot make the check run
// away.
func (c *checker) parts(value any, seen map[string]bool, entered *[]string) []map[string]any {
	object, ok := value.(map[string]any)
	if !ok || c.work > maxWork {
		return nil
	}
	c.work++
	var out []map[string]any
	if ref, ok := object["$ref"].(string); ok {
		if strings.HasPrefix(ref, "#/") && !seen[ref] {
			seen[ref] = true
			if entered != nil {
				*entered = append(*entered, ref)
			}
			out = append(out, c.parts(c.pointer(ref), seen, entered)...)
			delete(seen, ref)
		}
		siblings := map[string]any{}
		for key, item := range object {
			if key != "$ref" {
				siblings[key] = item
			}
		}
		if len(siblings) == 0 {
			return out
		}
		object = siblings
	}
	out = append(out, object)
	members, _ := object["allOf"].([]any)
	for _, member := range members {
		out = append(out, c.parts(member, seen, entered)...)
	}
	return out
}

func (c *checker) hasFormat(value any, format string) bool {
	for _, schema := range c.parts(value, map[string]bool{}, nil) {
		if declared, _ := schema["format"].(string); declared == format {
			return true
		}
	}
	return false
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type shape int

const (
	undeclared shape = iota
	notList
	bareArray
	otherList
	envelope
)

func (c *checker) listShape(responses map[string]any) shape {
	response, ok := c.resolve(responses["200"]).(map[string]any)
	if !ok {
		return notList
	}
	content, _ := response["content"].(map[string]any)
	media, isJSON := content["application/json"].(map[string]any)
	if !isJSON {
		if len(content) > 0 {
			return notList
		}
		return undeclared
	}
	parts := c.parts(media["schema"], map[string]bool{}, nil)
	if len(parts) == 0 || len(parts) == 1 && len(parts[0]) == 0 {
		return undeclared
	}
	properties := map[string]any{}
	required := map[string]bool{}
	for _, part := range parts {
		if isArray(part) || c.offersArray(part, map[string]bool{}) {
			return bareArray
		}
		declared, _ := part["properties"].(map[string]any)
		for name, property := range declared {
			properties[name] = property
		}
		names, _ := part["required"].([]any)
		for _, name := range names {
			if text, ok := name.(string); ok {
				required[text] = true
			}
		}
	}
	items := c.parts(properties["items"], map[string]bool{}, nil)
	if len(items) > 0 && anyArray(items) {
		if required["items"] && onlyArrays(items) {
			return envelope
		}
		return otherList
	}
	if anyArray(c.parts(properties["data"], map[string]bool{}, nil)) {
		return otherList
	}
	return notList
}

// offersArray reports whether one of the schema's anyOf or oneOf options,
// at any depth, is an array, so the response can be a bare list.
func (c *checker) offersArray(schema map[string]any, seen map[string]bool) bool {
	for _, key := range []string{"anyOf", "oneOf"} {
		options, _ := schema[key].([]any)
		for _, option := range options {
			var entered []string
			parts := c.parts(option, seen, &entered)
			if anyArray(parts) {
				return true
			}
			inner := extend(seen, entered)
			for _, part := range parts {
				if c.offersArray(part, inner) {
					return true
				}
			}
		}
	}
	return false
}

func anyArray(parts []map[string]any) bool {
	for _, part := range parts {
		if isArray(part) {
			return true
		}
	}
	return false
}

// onlyArrays reports whether every typed part allows arrays and nothing else.
func onlyArrays(parts []map[string]any) bool {
	for _, part := range parts {
		switch declared := part["type"].(type) {
		case string:
			if declared != "array" {
				return false
			}
		case []any:
			if len(declared) != 1 || declared[0] != "array" {
				return false
			}
		}
		if nullable, _ := part["nullable"].(bool); nullable {
			return false
		}
	}
	return true
}

func hasType(schema map[string]any, kind string) bool {
	switch declared := schema["type"].(type) {
	case string:
		return declared == kind
	case []any:
		for _, entry := range declared {
			if entry == kind {
				return true
			}
		}
	}
	return false
}

func hasAny(query map[string]map[string]any, names []string) bool {
	for _, name := range names {
		if _, ok := query[name]; ok {
			return true
		}
	}
	return false
}

func isArray(schema map[string]any) bool {
	switch kind := schema["type"].(type) {
	case string:
		return kind == "array"
	case []any:
		for _, entry := range kind {
			if entry == "array" {
				return true
			}
		}
	}
	return false
}

func isTimeName(name string) bool {
	if timeNames[name] {
		return true
	}
	for _, suffix := range timeSuffix {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

func hasTimeFormat(schema map[string]any) bool {
	if format, _ := schema["format"].(string); format == "date" || format == "date-time" {
		return true
	}
	for _, key := range []string{"anyOf", "oneOf"} {
		options, _ := schema[key].([]any)
		for _, option := range options {
			if entry, ok := option.(map[string]any); ok && hasTimeFormat(entry) {
				return true
			}
		}
	}
	return false
}

// resolveSchema returns one schema with the keywords of every part that
// applies to value, so a keyword beside a $ref or inside allOf is seen.
func (c *checker) resolveSchema(value any) map[string]any {
	merged := map[string]any{}
	for _, part := range c.parts(value, map[string]bool{}, nil) {
		for key, item := range part {
			merged[key] = item
		}
	}
	return merged
}

func (c *checker) resolve(value any) any {
	for depth := 0; depth < 16; depth++ {
		object, ok := value.(map[string]any)
		if !ok {
			return value
		}
		ref, ok := object["$ref"].(string)
		if !ok || !strings.HasPrefix(ref, "#/") {
			return value
		}
		value = c.pointer(ref)
	}
	return value
}

func (c *checker) pointer(ref string) any {
	var current any = c.document
	for _, segment := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		segment = strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
		container, _ := current.(map[string]any)
		current = container[segment]
	}
	return current
}
