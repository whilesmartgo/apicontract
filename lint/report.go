package lint

import (
	"fmt"
	"io"
	"strings"
)

// WriteReport writes the accepted violations as a Markdown table, so a
// document can track the routes that do not meet the contract yet.
func WriteReport(w io.Writer, accepted []Violation, exceptions []Exception) error {
	if _, err := fmt.Fprintln(w, "| Spec | Route | Rule | Parameter | Reason |"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "| --- | --- | --- | --- | --- |"); err != nil {
		return err
	}
	for _, violation := range accepted {
		reason := ""
		for _, exception := range exceptions {
			if exception.matches(violation) {
				reason = exception.Reason
				break
			}
		}
		route := strings.TrimSpace(violation.Method + " " + violation.Path)
		if _, err := fmt.Fprintf(w, "| %s | %s | %s | %s | %s |\n", cell(violation.Spec), cell(route), violation.Rule, cell(violation.Subject), strings.ReplaceAll(reason, "|", `\|`)); err != nil {
			return err
		}
	}
	return nil
}

func cell(value string) string {
	if value == "" {
		return ""
	}
	return "`" + strings.ReplaceAll(value, "|", `\|`) + "`"
}
