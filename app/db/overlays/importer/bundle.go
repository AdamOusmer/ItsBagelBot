// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package importer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"ItsBagelBot/internal/domain/overlays"
	"ItsBagelBot/pkg/codec"
)

const bundleFile = "widget.bundle.json"

type FieldType string

const (
	FieldText    FieldType = "text"
	FieldColor   FieldType = "color"
	FieldNumber  FieldType = "number"
	FieldBoolean FieldType = "boolean"
	FieldSelect  FieldType = "select"
	FieldImage   FieldType = "image"
	FieldVideo   FieldType = "video"
	FieldSound   FieldType = "sound"
	FieldFont    FieldType = "font"
)

var fieldTypes = map[string]FieldType{
	"text":        FieldText,
	"colorpicker": FieldColor,
	"number":      FieldNumber,
	"slider":      FieldNumber,
	"checkbox":    FieldBoolean,
	"dropdown":    FieldSelect,
	"image-input": FieldImage,
	"video-input": FieldVideo,
	"sound-input": FieldSound,
	"googlefont":  FieldFont,
}

var codeChecks = []struct {
	reason  Reason
	pattern *regexp.Regexp
}{
	{ReasonRemoteFont, regexp.MustCompile(`(?i)fonts\.(googleapis|gstatic)\.com`)},
	{ReasonNetwork, regexp.MustCompile(`(?i)\bfetch\s*\(|XMLHttpRequest|WebSocket|EventSource|sendBeacon|<script[^>]+src=|\$\.(get|post|ajax|getJSON)\s*\(`)},
	{ReasonKeyValue, regexp.MustCompile(`\.store\.(get|set|del|delete)\w*\s*\(`)},
	{ReasonTips, regexp.MustCompile(`(?i)\b(tip|merch)[-_]?(latest|recent|goal|session|total|count)`)},
	{ReasonStore, regexp.MustCompile(`(?i)redemption-|\bloyalty\b`)},
	{ReasonContests, regexp.MustCompile(`(?i)\b(contest|bet)s?-`)},
	{ReasonMediaShare, regexp.MustCompile(`(?i)media-?share`)},
}

type rawField struct {
	Type    string   `json:"type"`
	Label   string   `json:"label"`
	Value   any      `json:"value"`
	Options any      `json:"options"`
	Min     *float64 `json:"min"`
	Max     *float64 `json:"max"`
	Step    *float64 `json:"step"`
}

type widgetSource struct {
	Name     string              `json:"-"`
	Dir      string              `json:"-"`
	Oversize bool                `json:"-"`
	HTML     string              `json:"html"`
	CSS      string              `json:"css"`
	JS       string              `json:"js"`
	Fields   map[string]rawField `json:"fields"`
	Data     map[string]any      `json:"data"`
}

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type BundleField struct {
	Name    string    `json:"name"`
	Type    FieldType `json:"type"`
	Label   string    `json:"label"`
	Options []Option  `json:"options,omitempty"`
	Min     *float64  `json:"min,omitempty"`
	Max     *float64  `json:"max,omitempty"`
	Step    *float64  `json:"step,omitempty"`
}

type Bundle struct {
	HTML   string        `json:"html"`
	CSS    string        `json:"css"`
	JS     string        `json:"js"`
	Fields []BundleField `json:"fields"`
}

func (r rawField) options() []Option {
	switch node := r.Options.(type) {
	case map[string]any:
		return optionsOf(slices.Sorted(maps.Keys(node)), func(key string) Option {
			return Option{Value: key, Label: fmt.Sprint(node[key])}
		})
	case []any:
		items := make([]string, len(node))
		for i, item := range node {
			items[i] = fmt.Sprint(item)
		}
		return optionsOf(items, func(item string) Option { return Option{Value: item, Label: item} })
	}
	return nil
}

func optionsOf(keys []string, build func(string) Option) []Option {
	options := make([]Option, len(keys))
	for i, key := range keys {
		options[i] = build(key)
	}
	return options
}

func (r rawField) kind() (FieldType, bool) {
	kind, known := fieldTypes[strings.ToLower(r.Type)]
	if !known {
		return FieldText, false
	}
	return kind, true
}

func (s widgetSource) code() string {
	return s.HTML + "\n" + s.CSS + "\n" + s.JS
}

func (s widgetSource) refusal() Reason {
	switch {
	case s.Oversize:
		return ReasonTooLarge
	case !utf8.ValidString(s.code()) || strings.ContainsRune(s.code(), 0):
		return ReasonNotText
	}
	return ""
}

func (b *builder) readWidgetFolder() (widgetSource, bool, error) {
	anchor, ok := b.shallowest("widget.html")
	if !ok {
		return widgetSource{}, false, nil
	}
	dir := path.Dir(anchor.Path)
	src := widgetSource{Name: sourceName(dir), Dir: dir}
	targets := []struct {
		file string
		into *string
	}{{"widget.html", &src.HTML}, {"widget.css", &src.CSS}, {"widget.js", &src.JS}}
	for _, target := range targets {
		text, oversize, err := b.text(fileRef{dir: dir, name: target.file})
		if err != nil {
			return src, false, err
		}
		*target.into = text
		src.Oversize = src.Oversize || oversize
	}
	if err := b.jsonFile(fileRef{dir: dir, name: "fields.json"}, &src.Fields); err != nil {
		return src, false, err
	}
	return src, true, b.jsonFile(fileRef{dir: dir, name: "data.json"}, &src.Data)
}

func sourceName(dir string) string {
	if dir == "." {
		return "Custom widget"
	}
	return truncate(path.Base(dir), maxName)
}

type fileRef struct {
	dir  string
	name string
}

func (b *builder) text(ref fileRef) (string, bool, error) {
	e, ok := b.take(ref.dir, ref.name)
	if !ok {
		return "", false, nil
	}
	data, within, err := e.read(codeLimit)
	return string(data), !within, err
}

func (b *builder) jsonFile(ref fileRef, into any) error {
	e, ok := b.take(ref.dir, ref.name)
	if !ok {
		return nil
	}
	data, within, err := e.read(codeLimit)
	if err != nil {
		return err
	}
	if !within || codec.Unmarshal(data, into) != nil {
		return &Error{Code: CodeInvalidBundle, Path: e.Path}
	}
	return nil
}

func (b *builder) customSettings(src widgetSource) (map[string]any, bool, error) {
	name := Subject(src.Name)
	if len(b.widgets) >= overlays.MaxWidgets {
		b.report.unsupported(name, ReasonTooManyWidgets)
		return nil, false, nil
	}
	if reason := src.refusal(); reason != "" {
		b.report.unsupported(name, reason)
		return nil, false, nil
	}
	b.scanCode(name, src)
	fields, values := b.resolveFields(src)
	data, err := encodeBundle(Bundle{HTML: src.HTML, CSS: src.CSS, JS: src.JS, Fields: fields})
	if err != nil {
		return nil, false, &Error{Code: CodeInvalidBundle, Path: src.Name}
	}
	if reason := b.sizeReason(overlays.AssetWidget, int64(len(data))); reason != "" {
		b.report.unsupported(name, reason)
		return nil, false, nil
	}
	sum := sha256.Sum256(data)
	id := b.add(File{
		Path: path.Join(src.Dir, bundleFile), Type: overlays.AssetWidget, SHA256: hex.EncodeToString(sum[:]),
		Size: int64(len(data)), Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil },
	})
	return map[string]any{"bundle": assetPrefix + id, "values": values}, true, nil
}

func encodeBundle(bundle Bundle) ([]byte, error) {
	var buf bytes.Buffer
	encoder := codec.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(bundle)
	return buf.Bytes(), err
}

func (b *builder) scanCode(name Subject, src widgetSource) {
	code := src.code()
	for _, check := range codeChecks {
		if check.pattern.MatchString(code) {
			b.report.unsupported(name, check.reason)
		}
	}
}

func (b *builder) resolveFields(src widgetSource) ([]BundleField, map[string]any) {
	names := slices.Sorted(maps.Keys(src.Fields))
	fields := make([]BundleField, 0, len(names))
	values := make(map[string]any, len(names))
	for _, name := range names {
		field, value := b.resolveField(src, name)
		fields = append(fields, field)
		values[name] = value
	}
	return fields, values
}

func (b *builder) resolveField(src widgetSource, name string) (BundleField, any) {
	raw := src.Fields[name]
	subject := Subject(src.Name + "/" + name)
	kind, known := raw.kind()
	if !known {
		b.report.approximated(subject, ReasonUnknownField)
	}
	value, has := src.Data[name]
	if !has {
		value = raw.Value
	}
	field := BundleField{Name: name, Type: kind, Label: raw.Label, Options: raw.options(), Min: raw.Min, Max: raw.Max, Step: raw.Step}
	return field, b.fieldValue(subject, kind, value)
}

func (b *builder) fieldValue(subject Subject, kind FieldType, value any) any {
	switch kind {
	case FieldImage, FieldVideo, FieldSound:
		return b.bind(subject, value)
	case FieldFont:
		return b.fontValue(subject, value)
	}
	return value
}

func (b *builder) fontValue(subject Subject, value any) any {
	family, ok := value.(string)
	if !ok {
		return value
	}
	name, exact := nearestFont(family)
	if !exact {
		b.report.approximated(subject, ReasonApproximatedFont)
	}
	return name
}
