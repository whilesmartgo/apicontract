// Command apicontract checks OpenAPI documents against the API contract.
//
//	apicontract lint [-exceptions file] [-seed reason] [-report file] openapi.json...
//	apicontract version
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/whilesmartgo/apicontract"
	"github.com/whilesmartgo/apicontract/lint"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: apicontract lint|version")
		return 2
	}
	switch args[0] {
	case "version":
		_, _ = fmt.Fprintln(stdout, apicontract.Version)
		return 0
	case "lint":
		return runLint(args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		return 2
	}
}

func runLint(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("lint", flag.ContinueOnError)
	flags.SetOutput(stderr)
	exceptionsPath := flags.String("exceptions", "", "JSON file of accepted violations, each with a reason")
	seed := flags.String("seed", "", "rewrite the exceptions file to accept every current violation, giving new entries this reason")
	reportPath := flags.String("report", "", "write accepted violations as a Markdown table to this file")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	specs := flags.Args()
	if len(specs) == 0 {
		_, _ = fmt.Fprintln(stderr, "lint: name at least one OpenAPI document")
		return 2
	}

	var violations []lint.Violation
	for _, spec := range specs {
		data, err := os.ReadFile(spec)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 2
		}
		found, err := lint.Check(spec, data)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 2
		}
		violations = append(violations, found...)
	}

	var exceptions []lint.Exception
	if *exceptionsPath != "" {
		loaded, err := lint.ReadExceptions(*exceptionsPath)
		switch {
		case err == nil:
			exceptions = loaded
		case errors.Is(err, os.ErrNotExist) && *seed != "":
		default:
			_, _ = fmt.Fprintln(stderr, err)
			return 2
		}
	}

	if *seed != "" {
		if *exceptionsPath == "" {
			_, _ = fmt.Fprintln(stderr, "lint: -seed needs -exceptions")
			return 2
		}
		exceptions = lint.Seed(violations, exceptions, specs, *seed)
		if err := lint.WriteExceptions(*exceptionsPath, exceptions); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 2
		}
	}

	open, accepted, stale := lint.Apply(violations, exceptions, specs)
	if *reportPath != "" {
		var report bytes.Buffer
		if err := lint.WriteReport(&report, accepted, exceptions); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 2
		}
		if err := os.WriteFile(*reportPath, report.Bytes(), 0o644); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 2
		}
	}
	for _, violation := range open {
		_, _ = fmt.Fprintln(stdout, violation)
	}
	for _, exception := range stale {
		_, _ = fmt.Fprintf(stdout, "stale exception, the violation is gone; remove it: %s\n", exception)
	}
	_, _ = fmt.Fprintf(stdout, "%s, %s, %d accepted by exceptions, %s\n", count(len(specs), "document"), count(len(open), "violation"), len(accepted), count(len(stale), "stale exception"))
	if len(open) > 0 || len(stale) > 0 {
		return 1
	}
	return 0
}

func count(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
