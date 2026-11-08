// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package grpc

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var rpcImplementations = []string{
	"google.golang.org/grpc",
	"google.golang.org/protobuf",
	"github.com/planetscale/vtprotobuf",
	"github.com/golang/protobuf",
}

var guardSkipDirs = map[string]bool{
	".git": true, ".claude": true, "node_modules": true,
	"web": true, "console": true, "docs": true, "vendor": true, "mail": true,
}

func TestOnlySharedPackageImportsRPCImplementations(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	offenders, err := findRPCImports(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Fatalf("only shared transport/serialization packages may import their owned implementations; extend their APIs instead:\n\t%s", strings.Join(offenders, "\n\t"))
	}
}

func findRPCImports(root string) ([]string, error) {
	var offenders []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if guardSkipDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !policeRPCFile(filepath.ToSlash(rel)) {
			return nil
		}
		imports, err := rpcImportsOf(path)
		if err != nil {
			return err
		}
		for _, imported := range imports {
			if allowedSerializationImport(filepath.ToSlash(rel), imported) {
				continue
			}
			offenders = append(offenders, rel+" imports "+imported)
		}
		return nil
	})
	return offenders, err
}

func allowedSerializationImport(file, imported string) bool {
	if !strings.HasPrefix(file, "pkg/protobuf/") {
		return false
	}
	for _, prefix := range []string{"google.golang.org/protobuf", "github.com/planetscale/vtprotobuf", "github.com/golang/protobuf"} {
		if imported == prefix || strings.HasPrefix(imported, prefix+"/") {
			return true
		}
	}
	return false
}

func policeRPCFile(relative string) bool {
	if !strings.HasSuffix(relative, ".go") || strings.HasPrefix(relative, "pkg/grpc/") {
		return false
	}
	// These exact-directory, test-only experiments intentionally measure raw vendor
	// baselines. Routing them through our wrapper would invalidate the comparison.
	// Production files and nested directories do not inherit this exception.
	const experiment = "app/desktop/internal/bridge/compare/"
	if strings.HasPrefix(relative, experiment) && strings.HasSuffix(relative, "_test.go") {
		return strings.Contains(strings.TrimPrefix(relative, experiment), "/")
	}
	return true
}

func rpcImportsOf(path string) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse imports %s: %w", path, err)
	}
	if ast.IsGenerated(file) {
		return nil, nil
	}
	var found []string
	for _, imported := range file.Imports {
		name, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			return nil, err
		}
		if isRPCImplementation(name) || productionFixtureImport(path, name) {
			found = append(found, name)
		}
	}
	return found, nil
}

func productionFixtureImport(file, imported string) bool {
	const fixture = "ItsBagelBot/pkg/grpc/testkit"
	return !strings.HasSuffix(file, "_test.go") && (imported == fixture || strings.HasPrefix(imported, fixture+"/"))
}

func TestRPCFixturesAreTestOnly(t *testing.T) {
	for _, test := range []struct {
		file, imported string
		blocked        bool
	}{
		{"app/desktop/main.go", "ItsBagelBot/pkg/grpc/testkit", true},
		{"app/desktop/bridge_test.go", "ItsBagelBot/pkg/grpc/testkit", false},
		{"app/service/server.go", "ItsBagelBot/pkg/grpc/testkit/nested", true},
		{"app/service/server.go", "ItsBagelBot/pkg/grpc/testkit-extra", false},
	} {
		if got := productionFixtureImport(test.file, test.imported); got != test.blocked {
			t.Errorf("fixture import %s in %s: blocked=%v", test.imported, test.file, got)
		}
	}
}

func isRPCImplementation(path string) bool {
	for _, prefix := range rpcImplementations {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func TestRPCGuardScope(t *testing.T) {
	for _, test := range []struct {
		path   string
		police bool
	}{
		{"app/service/main.go", true},
		{"app/service/main_test.go", true},
		{"pkg/another/helper.go", true},
		{"pkg/grpc/client.go", false},
		{"pkg/grpc/internal/helper.go", false},
		{"app/desktop/internal/bridge/compare/network_test.go", false},
		{"app/desktop/internal/bridge/compare/model.go", true},
		{"app/desktop/internal/bridge/compare/nested/network_test.go", true},
		{"app/desktop/internal/bridge/compare-extra/network_test.go", true},
	} {
		if got := policeRPCFile(test.path); got != test.police {
			t.Errorf("police(%q)=%v, want %v", test.path, got, test.police)
		}
	}
}

func TestRPCGuardRecognizesGeneratedCode(t *testing.T) {
	root := t.TempDir()
	generated := filepath.Join(root, "event.pb.go")
	source := []byte("// Code generated by protoc-gen-go. DO NOT EDIT.\n\npackage fixture\nimport \"google.golang.org/protobuf/proto\"\n")
	if err := os.WriteFile(generated, source, 0600); err != nil {
		t.Fatal(err)
	}
	imports, err := rpcImportsOf(generated)
	if err != nil || len(imports) != 0 {
		t.Fatalf("generated file: %v %v", imports, err)
	}
	handwritten := filepath.Join(root, "handwritten.go")
	source = []byte("package fixture\nimport \"google.golang.org/grpc/metadata\"\n")
	if err := os.WriteFile(handwritten, source, 0600); err != nil {
		t.Fatal(err)
	}
	imports, err = rpcImportsOf(handwritten)
	if err != nil || len(imports) != 1 {
		t.Fatalf("handwritten file: %v %v", imports, err)
	}
}

func TestRPCGuardVendorPrefixes(t *testing.T) {
	for _, name := range []string{"google.golang.org/grpc", "google.golang.org/grpc/metadata", "google.golang.org/protobuf/proto", "github.com/planetscale/vtprotobuf/protohelpers", "github.com/golang/protobuf/proto"} {
		if !isRPCImplementation(name) {
			t.Errorf("unpoliced implementation %q", name)
		}
	}
	for _, name := range []string{"ItsBagelBot/pkg/grpc", "google.golang.org/grpc-extra", "google.golang.org/protobuf-extra", "github.com/planetscale/vtprotobuf-extra"} {
		if isRPCImplementation(name) {
			t.Errorf("unrelated package rejected: %q", name)
		}
	}
}

func TestSerializationGuardScope(t *testing.T) {
	for _, entry := range []struct {
		file, imported string
		allowed        bool
	}{
		{"pkg/protobuf/protobuf.go", "google.golang.org/protobuf/encoding/protowire", true},
		{"pkg/protobuf/protobuf_test.go", "github.com/planetscale/vtprotobuf/protohelpers", true},
		{"pkg/protobuf/protobuf.go", "google.golang.org/grpc/mem", false},
		{"pkg/protobuf-extra/protobuf.go", "google.golang.org/protobuf/proto", false},
		{"app/protobuf/main.go", "google.golang.org/protobuf/proto", false},
		{"pkg/protobuf/protobuf.go", "google.golang.org/protobuf-extra/proto", false},
	} {
		if got := allowedSerializationImport(entry.file, entry.imported); got != entry.allowed {
			t.Fatalf("serialization guard %s %s: %v", entry.file, entry.imported, got)
		}
	}
}
