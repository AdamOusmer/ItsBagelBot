// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package buildguard

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var handScanPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{
		"a string search for a literal '{' span",
		regexp.MustCompile(`(strings|bytes)\.(Index|LastIndex|Contains|Cut|CutPrefix|Split|SplitN|TrimPrefix|HasPrefix)[A-Za-z]*\([^\n]*"\{`),
	},
	{
		"a regular expression matching a '{' span",
		regexp.MustCompile(`\\\{`),
	},
}

func scanExempt(rel string) bool {
	return strings.HasPrefix(rel, "pkg/tmpl/") || strings.HasSuffix(rel, "_test.go")
}

const minScannedFiles = 500

func TestNoHandRolledTokenScanners(t *testing.T) {
	files := goSources(t, "../..")
	if len(files) < minScannedFiles {
		t.Fatalf("scanned %d Go files, want at least %d: the walk root or the prune list is wrong, not the tree", len(files), minScannedFiles)
	}
	var findings []string
	for _, path := range files {
		findings = append(findings, scanFile(t, path)...)
	}
	if len(findings) > 0 {
		t.Fatalf("hand-rolled {token} scanning outside pkg/tmpl:\n  %s\n\nUse tmpl.Lex (or tmpl.Expand) instead: it hands back the span's Name, Payload and Fallback already split, so a grammar change reaches every surface at once.",
			strings.Join(findings, "\n  "))
	}
}

func scanFile(t *testing.T, path string) []string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out []string
	for i, line := range strings.Split(string(body), "\n") {
		if name, hit := matchHandScan(line); hit {
			out = append(out, filepath.ToSlash(path)+":"+strconv.Itoa(i+1)+": "+name)
		}
	}
	return out
}

func TestHandScanPatternsMatch(t *testing.T) {
	for _, line := range []string{
		`idx := strings.Index(t.haystack[at:], "{urlfetch:")`,
		`if strings.HasPrefix(key, "{") {`,
		`name, _ := strings.CutPrefix(s, "{counter:")`,
		"re := regexp.MustCompile(`\\{([a-z]+)\\}`)",
	} {
		if _, hit := matchHandScan(line); !hit {
			t.Errorf("hand scanner not caught: %s", line)
		}
	}
	for _, line := range []string{
		`pairs = append(pairs, "{"+kv[i]+"}", kv[i+1])`,
		`return strings.TrimPrefix(raider, "@"), true`,
		`if s[i] != '{' {`,
		`fmt.Sprintf("%s{%d}", name, n)`,
	} {
		if name, hit := matchHandScan(line); hit {
			t.Errorf("false positive (%s): %s", name, line)
		}
	}
}

func matchHandScan(line string) (string, bool) {
	for _, p := range handScanPatterns {
		if p.re.MatchString(line) {
			return p.name, true
		}
	}
	return "", false
}

func goSources(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := relSlash(root, path)
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			return skipDir(d.Name())
		}
		if strings.HasSuffix(path, ".go") && !scanExempt(rel) {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

var foreignTrees = map[string]bool{"vendor": true, "node_modules": true}

func skipDir(name string) error {
	dotted := len(name) > 1 && strings.HasPrefix(name, ".")
	if foreignTrees[name] || dotted {
		return fs.SkipDir
	}
	return nil
}

func relSlash(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}
