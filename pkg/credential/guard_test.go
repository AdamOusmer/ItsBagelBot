// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package credential

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestOSStoreImplementationStaysShared(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(root, func(file string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if skipGuardDirectory(entry.Name()) {
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
		return guardCredentialImports(file, filepath.ToSlash(relative))
	})
	if err != nil {
		t.Fatal(err)
	}
}
func skipGuardDirectory(name string) bool {
	switch name {
	case ".git", ".claude", "node_modules", "vendor", "_build", "deps":
		return true
	}
	return false
}
func guardCredentialImports(file, relative string) error {
	source, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
	if err != nil {
		return err
	}
	for _, value := range source.Imports {
		imported, err := strconv.Unquote(value.Path.Value)
		if err != nil {
			return err
		}
		if rawCredentialImport(imported) && !strings.HasPrefix(relative, "pkg/credential/") {
			return &boundaryError{file: relative}
		}
	}
	return nil
}

type boundaryError struct{ file string }

func (e *boundaryError) Error() string {
	return "use pkg/credential instead of raw OS credential implementation: " + e.file
}
func rawCredentialImport(value string) bool {
	for _, prefix := range []string{"github.com/zalando/go-keyring", "github.com/99designs/keyring", "github.com/danieljoos/wincred"} {
		if value == prefix || strings.HasPrefix(value, prefix+"/") {
			return true
		}
	}
	return false
}
