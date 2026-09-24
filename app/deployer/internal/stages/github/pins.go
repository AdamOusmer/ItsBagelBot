// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

type PinLine struct {
	File     ports.FilePath
	Line     int
	Image    deploy.ImageName
	Pin      deploy.ImagePin
	Kind     string
	Workload string
}

var (
	tagPattern    = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type imageRepo string

func (r imageRepo) pinPattern() *regexp.Regexp {
	return regexp.MustCompile(`^\s*(?:-\s+)?image:\s*["']?` + regexp.QuoteMeta(string(r)) +
		`/([a-z0-9][a-z0-9._/-]*):([A-Za-z0-9_][A-Za-z0-9._-]{0,127})@(sha256:[0-9a-f]{64})`)
}

func ParsePins(files ports.Files, repo string) []PinLine {
	re := imageRepo(repo).pinPattern()
	var out []PinLine
	for _, p := range sortedPaths(files) {
		out = append(out, parseFile(p, files[p], re)...)
	}
	return out
}

func parseFile(p ports.FilePath, body []byte, re *regexp.Regexp) []PinLine {
	var doc docScanner
	var out []PinLine
	for i, line := range strings.Split(string(body), "\n") {
		doc.feed(line)
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		out = append(out, PinLine{
			File: p, Line: i + 1, Image: deploy.ImageName(m[1]),
			Pin:  deploy.ImagePin{Tag: deploy.Tag(m[2]), Digest: deploy.Digest(m[3])},
			Kind: doc.kind, Workload: doc.name,
		})
	}
	return out
}

func RewritePins(files ports.Files, repo string, pins map[deploy.ImageName]deploy.ImagePin) (ports.Files, error) {
	if err := validPins(pins); err != nil {
		return nil, err
	}
	re := imageRepo(repo).pinPattern()
	out := ports.Files{}
	for p, body := range files {
		if next, changed := rewriteFile(body, re, pins); changed {
			out[p] = next
		}
	}
	return out, nil
}

func rewriteFile(body []byte, re *regexp.Regexp, pins map[deploy.ImageName]deploy.ImagePin) ([]byte, bool) {
	lines := strings.Split(string(body), "\n")
	changed := false
	for i, line := range lines {
		lines[i] = rewriteLine(line, re, pins)
		changed = changed || lines[i] != line
	}
	return []byte(strings.Join(lines, "\n")), changed
}

func rewriteLine(line string, re *regexp.Regexp, pins map[deploy.ImageName]deploy.ImagePin) string {
	m := re.FindStringSubmatchIndex(line)
	if m == nil {
		return line
	}
	pin, ok := pins[deploy.ImageName(line[m[2]:m[3]])]
	if !ok {
		return line
	}
	return line[:m[4]] + string(pin.Tag) + "@" + string(pin.Digest) + line[m[7]:]
}

func validPins(pins map[deploy.ImageName]deploy.ImagePin) error {
	for image, pin := range pins {
		if !tagPattern.MatchString(string(pin.Tag)) {
			return fmt.Errorf("%w: %s: tag %q", ports.ErrInvalid, image, pin.Tag)
		}
		if !digestPattern.MatchString(string(pin.Digest)) {
			return fmt.Errorf("%w: %s: digest %q", ports.ErrInvalid, image, pin.Digest)
		}
	}
	return nil
}

type docScanner struct {
	kind   string
	name   string
	inMeta bool
}

func (d *docScanner) feed(line string) {
	switch {
	case strings.HasPrefix(line, "---"):
		*d = docScanner{}
	case strings.HasPrefix(line, "kind:"):
		d.kind = yamlScalar(line[len("kind:"):])
	case line == "metadata:":
		d.inMeta = true
	case d.inMeta:
		d.metaLine(line)
	}
}

func (d *docScanner) metaLine(line string) {
	if line == "" || strings.HasPrefix(line, "#") {
		return
	}
	if !strings.HasPrefix(line, " ") {
		d.inMeta = false
		return
	}
	if d.name == "" && strings.HasPrefix(line, "  name:") {
		d.name = yamlScalar(line[len("  name:"):])
	}
}

func yamlScalar(s string) string { return strings.Trim(strings.TrimSpace(s), `"'`) }

func sortedPaths(files ports.Files) []ports.FilePath {
	paths := make([]ports.FilePath, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	return paths
}

func pinsByImage(lines []PinLine) map[deploy.ImageName]deploy.ImagePin {
	out := map[deploy.ImageName]deploy.ImagePin{}
	for _, l := range lines {
		if _, seen := out[l.Image]; !seen {
			out[l.Image] = l.Pin
		}
	}
	return out
}

var rolloutKinds = []string{"Deployment", "DaemonSet"}

func servicesFor(lines []PinLine, images map[deploy.ImageName]deploy.ImagePin) []string {
	var out []string
	for _, l := range lines {
		_, pinned := images[l.Image]
		if pinned && slices.Contains(rolloutKinds, l.Kind) {
			out = append(out, l.Workload)
		}
	}
	return slices.Compact(out)
}
