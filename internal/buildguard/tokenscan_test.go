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

// Hand-rolled "{token}" scanners. Decision record.
//
// pkg/tmpl exists because two of them had already grown independently
// (sesame's module.Expand and outgress's expandTokens) and disagreed about
// case folding on a payload. Adopting the lexer everywhere removed four more:
// the urlfetch referrer scan in app/db (which missed the '|' fallback
// grammar, so deleting a definition reported zero referrers and broke live
// commands), a strings.NewReplacer over "{name}" literals in the raffle
// announcer, and the two reward repl switches that re-derived nothing but
// resolved a different palette than their neighbours.
//
// Every one of those was cheap to write and correct on the day it was
// written. What makes them expensive is that the grammar kept moving — the
// '|' fallback, {if:…}, {n:} rest-of-args — and a substring scan does not
// fail when the grammar grows past it; it silently answers the old question.
// Nothing in the compiler notices, and the symptom surfaces months later as
// "the bot stopped expanding my token".
//
// So this test is the thing that notices. It greps the Go tree for the two
// shapes a hand scanner takes and fails with file:line, which costs one
// commit to fix at authoring time instead of one incident to find later.
//
// The rule is not "never touch a brace": it is "if you are looking for a
// span, call tmpl.Lex". A caller that needs the name, the payload or the
// fallback of a "{...}" span already has all three on the Token.

// handScanPatterns are the two shapes, with the name each failure reports.
//
// braceLiteralCall is a string/bytes search whose needle STARTS with '{':
// strings.Index(s, "{urlfetch:"), strings.HasPrefix(key, "{"), and the
// TrimPrefix/Cut family that a scanner reaches for next. The literal has to
// start with the brace — "}" alone, or a brace in the middle of a message, is
// ordinary text handling and not a parser.
//
// escapedBraceRegexp is a regular-expression source escaping a brace ("\{").
// A brace only needs escaping in a pattern that is matching one, which means
// the pattern is parsing spans.
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

// scanExempt reports whether a path is allowed to hold the shapes above.
//
// Exactly two entries, and neither is a suppression of a real finding:
//
//   - pkg/tmpl is the lexer. It is the one place that IS allowed to read the
//     grammar byte by byte; that is the whole point of concentrating it.
//   - _test.go, which also covers this file: it spells both patterns as
//     literals in order to search for them, so it matches itself.
//
// _test.go files are skipped wholesale for a second reason too: a table test
// writes literal spans ("{urlfetch:weather|n/a}") as DATA, and a guard that
// flagged those would push the tables into indirection to stay quiet, which
// makes the tests worse to read for no gain. A hand scanner is production
// code — that is where this looks.
func scanExempt(rel string) bool {
	return strings.HasPrefix(rel, "pkg/tmpl/") || strings.HasSuffix(rel, "_test.go")
}

// minScannedFiles keeps a guard that scans NOTHING from passing. The root is
// a relative path, so a package move or a walk that prunes too eagerly would
// otherwise turn this test green by finding no files at all — the failure mode
// every tree-walking guard has. The repo holds thousands of .go files; 500 is
// a floor no plausible reorganisation crosses.
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

// scanFile returns one "file:line: shape" finding per offending line.
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

// TestHandScanPatternsMatch proves the guard above can actually fail. A
// tree-walking test that finds nothing looks identical whether the tree is
// clean or the patterns are broken, so the shapes are asserted here against
// the real lines this refactor deleted, plus the ordinary brace handling that
// must NOT trip it.
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

// matchHandScan names the first shape one source line hits.
func matchHandScan(line string) (string, bool) {
	for _, p := range handScanPatterns {
		if p.re.MatchString(line) {
			return p.name, true
		}
	}
	return "", false
}

// goSources walks root and returns the non-exempt .go files under it.
func goSources(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := relSlash(root, path)
		if d.IsDir() {
			// rel "." is the walk root itself. It is spelled "../.." here, so
			// its Name() is ".." and the dotted-directory rule below would
			// prune the entire tree on the first callback (it did, until
			// minScannedFiles caught it).
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

// foreignTrees are the dependency and tool caches that hold no first-party Go
// source.
var foreignTrees = map[string]bool{"vendor": true, "node_modules": true}

// skipDir prunes the trees that hold no first-party Go source: dependency and
// tool caches, and every dotted directory (.git, and the agent worktrees under
// .claude, which are whole copies of this repo and would be scanned twice).
func skipDir(name string) error {
	// ".git" and ".claude" prune, "." does not: that is the walk's own root.
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
