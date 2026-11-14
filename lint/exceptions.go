package lint

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Exception accepts violations that match it, with a reason. Method and Path
// may be "*" to cover a whole document. An empty Subject matches any subject.
type Exception struct {
	Spec    string `json:"spec"`
	Rule    string `json:"rule"`
	Method  string `json:"method,omitempty"`
	Path    string `json:"path,omitempty"`
	Subject string `json:"subject,omitempty"`
	Reason  string `json:"reason"`
}

func (e Exception) matches(v Violation) bool {
	return e.Spec == v.Spec &&
		(e.Rule == "*" || e.Rule == v.Rule) &&
		(e.Method == "*" || e.Method == v.Method) &&
		(e.Path == "*" || e.Path == v.Path) &&
		(e.Subject == "" || e.Subject == v.Subject)
}

func (e Exception) String() string {
	parts := []string{e.Spec, e.Rule}
	if e.Method != "" {
		parts = append(parts, e.Method+" "+e.Path)
	}
	if e.Subject != "" {
		parts = append(parts, e.Subject)
	}
	return strings.Join(parts, ": ")
}

// ReadExceptions loads an exceptions file. Every entry needs a reason.
func ReadExceptions(path string) ([]Exception, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var exceptions []Exception
	if err := json.Unmarshal(data, &exceptions); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i, exception := range exceptions {
		if strings.TrimSpace(exception.Reason) == "" {
			return nil, fmt.Errorf("%s: entry %d (%s) has no reason", path, i, exception)
		}
	}
	return exceptions, nil
}

// WriteExceptions writes exceptions sorted, one entry per violation.
func WriteExceptions(path string, exceptions []Exception) error {
	sort.SliceStable(exceptions, func(i, j int) bool {
		a, b := exceptions[i], exceptions[j]
		return strings.Join([]string{a.Spec, a.Path, a.Method, a.Rule, a.Subject}, "\x00") < strings.Join([]string{b.Spec, b.Path, b.Method, b.Rule, b.Subject}, "\x00")
	})
	data, err := json.MarshalIndent(exceptions, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Apply splits violations into those no exception accepts and those it does,
// and returns the exceptions that accept nothing. Stale exceptions fail a run
// so the list only shrinks.
func Apply(violations []Violation, exceptions []Exception, specs []string) (open, accepted []Violation, stale []Exception) {
	used := make([]bool, len(exceptions))
	for _, violation := range violations {
		matched := false
		for i, exception := range exceptions {
			if exception.matches(violation) {
				used[i], matched = true, true
			}
		}
		if matched {
			accepted = append(accepted, violation)
		} else {
			open = append(open, violation)
		}
	}
	checked := map[string]bool{}
	for _, spec := range specs {
		checked[spec] = true
	}
	for i, exception := range exceptions {
		if !used[i] && checked[exception.Spec] {
			stale = append(stale, exception)
		}
	}
	return open, accepted, stale
}

// Seed keeps the existing exceptions that still accept a violation, drops the
// stale ones, and adds an exception with reason for every open violation.
func Seed(violations []Violation, existing []Exception, specs []string, reason string) []Exception {
	open, _, stale := Apply(violations, existing, specs)
	var seeded []Exception
	for _, exception := range existing {
		if !containsException(stale, exception) {
			seeded = append(seeded, exception)
		}
	}
	for _, violation := range open {
		seeded = append(seeded, Exception{Spec: violation.Spec, Rule: violation.Rule, Method: violation.Method, Path: violation.Path, Subject: violation.Subject, Reason: reason})
	}
	return seeded
}

func containsException(list []Exception, target Exception) bool {
	for _, exception := range list {
		if exception == target {
			return true
		}
	}
	return false
}
