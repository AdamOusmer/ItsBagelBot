// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package svcboot

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// bootConstructors are the primitives a service main must not reach for
// directly. Each one had a hand-rolled copy in twelve mains before this
// package took them over, and each copy is a place the fleet's boot
// conventions can silently disagree with the other fourteen:
//
//   - logger.New decides APP_ENV's default, and the twelve copies defaulted to
//     development, so a production pod that forgot the variable logged verbose
//     and unsampled with nothing saying so (see resolveAppEnv).
//   - monitor.New has to be followed by WrapLogger and a deferred Shutdown, in
//     that order, or the APM agent reports nothing and drops its buffer.
//   - signal.NotifyContext has to be paired with a stop that runs last, and the
//     ctx it returns is what Await drains on.
//   - valkey.NewClient, db.NewDriver and bus.Connect each carry credential env
//     names and a fatal-on-failure contract that belong in one place.
var bootConstructors = map[string]map[string]string{
	"ItsBagelBot/pkg/logger": {"New": "svcboot.NewCore (or svcboot.NewLogger for a one-shot entrypoint)"},
	"ItsBagelBot/pkg/monitor": {
		"New":        "svcboot.NewCore",
		"WrapLogger": "svcboot.NewCore",
		"Shutdown":   "the cleanup func svcboot.NewCore returns",
	},
	"os/signal":              {"NotifyContext": "svcboot.NewCore, whose Ctx main blocks on via Await"},
	"ItsBagelBot/pkg/valkey": {"NewClient": "svcboot.MustValkey"},
	"ItsBagelBot/pkg/db":     {"NewDriver": "svcboot/databoot.MustEntDriver"},
	"ItsBagelBot/pkg/bus":    {"Connect": "svcboot.MustRPCConn or svcboot.MustNATS"},
}

// exempt maps a main.go, relative to the module root, to why it is allowed to
// call a boot constructor itself. Empty on purpose: all fifteen mains go
// through svcboot today, and the next exception should be a reviewed edit to
// this map rather than a quiet call in one service.
var exempt = map[string]string{}

// TestOnlySvcbootBootsAService is the standing guard behind the migration that
// put every app/**/main.go on this package. The value of one boot skeleton is
// that changing it — the APP_ENV default, the shutdown drain, a credential env
// name — is one edit; a hand-rolled preamble in one new service silently takes
// that back, and nothing else in the build would complain.
func TestOnlySvcbootBootsAService(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("locate module root: %v", err)
	}

	offenders, err := findBootCalls(filepath.Join(root, "app"), root)
	if err != nil {
		t.Fatalf("walk app: %v", err)
	}
	if len(offenders) > 0 {
		t.Fatalf("a service main must boot through pkg/svcboot:\n\t%s", strings.Join(offenders, "\n\t"))
	}
}

// findBootCalls reports every "<main.go> calls <pkg>.<Func>; use <replacement>"
// the rule forbids, over every main.go under appDir.
func findBootCalls(appDir, root string) ([]string, error) {
	var offenders []string
	err := filepath.WalkDir(appDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if notAServiceMain(d) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if _, ok := exempt[filepath.ToSlash(rel)]; ok {
			return nil
		}
		for _, call := range bootCallsIn(path) {
			offenders = append(offenders, filepath.ToSlash(rel)+" "+call)
		}
		return nil
	})
	sort.Strings(offenders)
	return offenders, err
}

// notAServiceMain reports whether a walk entry is something other than a
// service entrypoint. Only main.go is policed: the rule is about how a binary
// boots, and the packages a main wires may legitimately hold their own
// connections (app/twitch/sesame/wiring.go dials the lanes).
func notAServiceMain(d fs.DirEntry) bool {
	return d.IsDir() || d.Name() != "main.go"
}

// bootCallsIn returns the forbidden calls one file makes. A file that cannot be
// parsed yields nothing: the build reports that with a far better message than
// this test would.
func bootCallsIn(path string) []string {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	watched := watchedNames(file)
	if len(watched) == 0 {
		return nil
	}

	var found []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if offence, bad := violation(call.Fun, watched); bad {
			found = append(found, offence)
		}
		return true
	})
	return found
}

// watchedNames maps the local name each watched package is imported under (its
// alias, or the last path segment) to that package's import path. An import the
// rule says nothing about is left out, so a file importing none of them is
// skipped without walking its body.
func watchedNames(file *ast.File) map[string]string {
	names := map[string]string{}
	for _, imported := range file.Imports {
		importPath, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			continue
		}
		if _, watched := bootConstructors[importPath]; !watched {
			continue
		}
		names[localName(imported, importPath)] = importPath
	}
	return names
}

// localName is the identifier a file refers to an import by.
func localName(imported *ast.ImportSpec, importPath string) string {
	if imported.Name != nil {
		return imported.Name.Name
	}
	return path.Base(importPath)
}

// violation reports whether fun names a forbidden constructor, and how to say
// so. It matches on the import's local name rather than on the package's own
// name because the fleet aliases pkg/valkey (pkg_valkey) to keep it apart from
// the driver it wraps.
func violation(fun ast.Expr, watched map[string]string) (string, bool) {
	selector, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	replacement, ok := bootConstructors[watched[pkg.Name]][selector.Sel.Name]
	if !ok {
		return "", false
	}
	return "calls " + pkg.Name + "." + selector.Sel.Name + "; use " + replacement, true
}
