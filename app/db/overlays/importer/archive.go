// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package importer

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"

	"ItsBagelBot/internal/domain/overlays"
)

const (
	ratioFloor = 1 << 20
	zipMagic   = "PK\x03\x04"
)

var archiveExts = []string{".zip", ".rar", ".7z", ".tar", ".gz"}

type entry struct {
	Path   string
	Size   int64
	SHA256 string
	file   *zip.File
}

func (e entry) open() (io.ReadCloser, error) {
	return e.file.Open()
}

func (e entry) read(limit int64) ([]byte, bool, error) {
	rc, err := e.file.Open()
	if err != nil {
		return nil, false, &Error{Code: CodeInvalidArchive, Path: e.Path}
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, false, &Error{Code: CodeInvalidArchive, Path: e.Path}
	}
	return data, int64(len(data)) <= limit, nil
}

type scanner struct {
	n    int64
	head [4]byte
	sum  hash.Hash
}

func (s *scanner) Write(p []byte) (int, error) {
	if s.n < int64(len(s.head)) {
		copy(s.head[s.n:], p)
	}
	s.n += int64(len(p))
	return s.sum.Write(p)
}

type archive struct {
	budget int64
	seen   map[string]struct{}
}

func readArchive(r io.ReaderAt, size int64) ([]entry, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return nil, &Error{Code: CodeInvalidArchive}
	}
	if len(zr.File) > overlays.ImportMaxEntries {
		return nil, &Error{Code: CodeTooManyEntries}
	}
	a := archive{budget: overlays.ImportMaxUnpackedBytes, seen: make(map[string]struct{}, len(zr.File))}
	entries := make([]entry, 0, len(zr.File))
	for _, f := range zr.File {
		e, keep, err := a.admit(f)
		if err != nil {
			return nil, err
		}
		if keep {
			entries = append(entries, e)
		}
	}
	slices.SortFunc(entries, func(x, y entry) int { return strings.Compare(x.Path, y.Path) })
	return entries, nil
}

func (a *archive) admit(f *zip.File) (entry, bool, error) {
	if err := a.checkHeader(f); err != nil {
		return entry{}, false, err
	}
	if f.FileInfo().IsDir() || junk(f.Name) {
		return entry{}, false, nil
	}
	sc, err := a.measure(f)
	if err != nil {
		return entry{}, false, err
	}
	return entry{Path: f.Name, Size: sc.n, SHA256: hex.EncodeToString(sc.sum.Sum(nil)), file: f}, true, nil
}

func (a *archive) checkHeader(f *zip.File) error {
	if !safeName(f.Name) {
		return &Error{Code: CodeUnsafePath, Path: f.Name}
	}
	if _, duplicate := a.seen[f.Name]; duplicate {
		return &Error{Code: CodeInvalidArchive, Path: f.Name}
	}
	a.seen[f.Name] = struct{}{}
	if f.Mode()&fs.ModeSymlink != 0 {
		return &Error{Code: CodeSymlink, Path: f.Name}
	}
	if slices.Contains(archiveExts, strings.ToLower(path.Ext(f.Name))) {
		return &Error{Code: CodeNestedArchive, Path: f.Name}
	}
	return nil
}

func (a *archive) measure(f *zip.File) (*scanner, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, &Error{Code: CodeInvalidArchive, Path: f.Name}
	}
	defer rc.Close()
	allowed := min(a.budget, max(ratioFloor, overlays.ImportMaxRatio*int64(f.CompressedSize64)))
	sc := &scanner{sum: sha256.New()}
	if _, err := io.Copy(sc, io.LimitReader(rc, allowed+1)); err != nil {
		return nil, &Error{Code: CodeInvalidArchive, Path: f.Name}
	}
	if err := a.judge(f.Name, sc, allowed); err != nil {
		return nil, err
	}
	a.budget -= sc.n
	return sc, nil
}

func (a *archive) judge(name string, sc *scanner, allowed int64) error {
	switch {
	case sc.n > a.budget:
		return &Error{Code: CodeTooLarge, Path: name}
	case sc.n > allowed:
		return &Error{Code: CodeBombRatio, Path: name}
	case string(sc.head[:]) == zipMagic:
		return &Error{Code: CodeNestedArchive, Path: name}
	}
	return nil
}

func safeName(name string) bool {
	if strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return false
	}
	return !slices.ContainsFunc(strings.Split(strings.TrimSuffix(name, "/"), "/"), badSegment)
}

func badSegment(segment string) bool {
	return slices.Contains([]string{"", ".", ".."}, segment)
}

func junk(name string) bool {
	return strings.HasPrefix(name, "__MACOSX/") || slices.Contains([]string{".DS_Store", "Thumbs.db"}, path.Base(name))
}
