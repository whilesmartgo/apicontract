package apicontract_test

import (
	"os"
	"strings"
	"testing"
)

func TestReadmeShowsExampleOutputs(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"example_test.go", "cmd/apicontract/example_test.go"} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, output := range exampleOutputs(string(source)) {
			if !strings.Contains(string(readme), output) {
				t.Errorf("README.md does not show this output from %s:\n%s", path, output)
			}
		}
	}
}

func exampleOutputs(source string) []string {
	var outputs []string
	var current []string
	inside := false
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimLeft(line, "\t")
		switch {
		case trimmed == "// Output:":
			inside, current = true, nil
		case inside && strings.HasPrefix(trimmed, "//"):
			current = append(current, strings.TrimPrefix(strings.TrimPrefix(trimmed, "//"), " "))
		case inside:
			inside = false
			if len(current) > 0 {
				outputs = append(outputs, strings.Join(current, "\n")+"\n")
			}
		}
	}
	return outputs
}
