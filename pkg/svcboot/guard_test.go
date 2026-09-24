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

var exempt = map[string]string{}

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

func notAServiceMain(d fs.DirEntry) bool {
	return d.IsDir() || d.Name() != "main.go"
}

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

func localName(imported *ast.ImportSpec, importPath string) string {
	if imported.Name != nil {
		return imported.Name.Name
	}
	return path.Base(importPath)
}

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
