// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package websocket

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var websocketImplementations = []string{"github.com/coder/websocket", "github.com/gorilla/websocket", "github.com/fasthttp/websocket", "github.com/gobwas/ws", "nhooyr.io/websocket"}
var websocketGuardSkip = map[string]bool{".git": true, ".claude": true, "node_modules": true, "web": true, "console": true, "docs": true, "vendor": true, "mail": true}

func TestWebSocketImplementationsStayShared(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	var offenders []string
	err = filepath.WalkDir(root, func(file string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if websocketGuardSkip[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(file, ".go") {
			return nil
		}
		relative, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		imports, err := forbiddenWebSocketImports(file, filepath.ToSlash(relative))
		if err != nil {
			return err
		}
		offenders = append(offenders, imports...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Fatalf("use pkg/websocket instead of direct WebSocket implementations; fixtures are test-only:\n%s", strings.Join(offenders, "\n"))
	}
}
func forbiddenWebSocketImports(file, relative string) ([]string, error) {
	source, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return nil, err
	}
	if ast.IsGenerated(source) {
		return nil, nil
	}
	var offenders []string
	for _, imported := range source.Imports {
		value, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			return nil, err
		}
		if blockedWebSocketImport(relative, value) {
			offenders = append(offenders, relative+" imports "+value)
		}
	}
	return offenders, nil
}
func blockedWebSocketImport(file, imported string) bool {
	const fixture = "ItsBagelBot/pkg/websocket/testkit"
	if !strings.HasSuffix(file, "_test.go") && (imported == fixture || strings.HasPrefix(imported, fixture+"/")) {
		return true
	}
	return isWebSocketImplementation(imported) && !allowedWebSocketImplementationFile(file)
}
func isWebSocketImplementation(imported string) bool {
	for _, prefix := range websocketImplementations {
		if imported == prefix || strings.HasPrefix(imported, prefix+"/") {
			return true
		}
	}
	return false
}
func allowedWebSocketImplementationFile(file string) bool {
	if strings.HasPrefix(file, "pkg/websocket/") {
		return true
	}
	// Existing Discord gateway transport predates this package. This exact
	// directory is grandfathered; nested/new service packages are not exempt.
	directory := path.Dir(file)
	if directory == "app/discord/ingress/internal/gateway" {
		return true
	}
	// Direct transport benchmarks must measure the real vendor baseline.
	return directory == "app/desktop/internal/bridge/compare" && strings.HasSuffix(file, "_test.go")
}
func TestWebSocketGuardScope(t *testing.T) {
	for _, example := range []struct {
		file    string
		allowed bool
	}{
		{"pkg/websocket/client.go", true}, {"pkg/websocket/testkit/server.go", true},
		{"pkg/websocket-extra/client.go", false}, {"pkg/other/client.go", false},
		{"app/desktop/main.go", false}, {"app/desktop/native_test.go", false},
		{"app/discord/ingress/internal/gateway/dial.go", true}, {"app/discord/ingress/internal/gateway/nested/dial.go", false},
		{"app/desktop/internal/bridge/compare/network_test.go", true}, {"app/desktop/internal/bridge/compare/network.go", false}, {"app/desktop/internal/bridge/compare/nested/network_test.go", false},
	} {
		if got := allowedWebSocketImplementationFile(example.file); got != example.allowed {
			t.Fatalf("guard scope %s: %v", example.file, got)
		}
	}
}
func TestWebSocketFixturesStayTestOnly(t *testing.T) {
	for _, example := range []struct {
		file, imported string
		blocked        bool
	}{
		{"app/desktop/main.go", "ItsBagelBot/pkg/websocket/testkit", true},
		{"app/desktop/native_test.go", "ItsBagelBot/pkg/websocket/testkit", false},
		{"pkg/websocket/client.go", "ItsBagelBot/pkg/websocket/testkit", true},
		{"pkg/other/client.go", "ItsBagelBot/pkg/websocket/testkit/nested", true},
		{"pkg/other/client.go", "ItsBagelBot/pkg/websocket/testkit-extra", false},
	} {
		if got := blockedWebSocketImport(example.file, example.imported); got != example.blocked {
			t.Fatalf("fixture guard %s: %v", example.file, got)
		}
	}
}
func TestWebSocketGuardVendorPrefixes(t *testing.T) {
	for _, value := range websocketImplementations {
		if !isWebSocketImplementation(value) || !isWebSocketImplementation(value+"/internal") {
			t.Fatal("implementation prefix escaped guard")
		}
	}
	for _, value := range []string{"ItsBagelBot/pkg/websocket", "github.com/coder/websocket-extra", "nhooyr.io/websocket-extra"} {
		if isWebSocketImplementation(value) {
			t.Fatal("unrelated package blocked")
		}
	}
}
