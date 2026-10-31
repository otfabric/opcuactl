// SPDX-License-Identifier: MIT

package opcuactl

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestJSONOutputDocumented keeps docs/json-output.md in step with the records
// of internal/model.
func TestJSONOutputDocumented(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "json-output.md"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "internal", "model", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	tag := regexp.MustCompile("json:\"([^\",]+)")
	checked := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range tag.FindAllSubmatch(src, -1) {
			if string(m[1]) == "-" {
				continue
			}
			checked++
			if !strings.Contains(string(doc), "`"+string(m[1])+"`") {
				t.Errorf("%s: field %s is not in docs/json-output.md", f, m[1])
			}
		}
	}
	if checked == 0 {
		t.Fatal("no json tags found")
	}
}
