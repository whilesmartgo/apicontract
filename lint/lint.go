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
	methods    = []string{"get", "put", "post", "patch", "delete"}
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
	sort.SliceStable(c.found, func(i, j int) bool { return c.found[i].key() < c.found[j].key() })
	return c.found, nil
}

func (v Violation) key() string {
	return strings.Join([]string{v.Spec, v.Path, v.Method, v.Rule, v.Subject}, "\x00")
}

type checker struct {
	spec     string
	document map[string]any
	found    []Violation
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
	schema := c.resolveSchema(media["schema"])
	if len(schema) == 0 {
		return undeclared
	}
	if isArray(schema) {
		return bareArray
	}
	properties, _ := schema["properties"].(map[string]any)
	if isArray(c.resolveSchema(properties["items"])) {
		return envelope
	}
	if isArray(c.resolveSchema(properties["data"])) {
		return otherList
	}
	return notList
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

func (c *checker) resolveSchema(value any) map[string]any {
	schema, _ := c.resolve(value).(map[string]any)
	return schema
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
		var current any = c.document
		for _, segment := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			segment = strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
			container, _ := current.(map[string]any)
			current = container[segment]
		}
		value = current
	}
	return value
}
