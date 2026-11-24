// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package importer_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"reflect"
	"slices"
	"strings"
	"testing"

	"ItsBagelBot/app/db/overlays/importer"
	"ItsBagelBot/internal/domain/overlays"
	"ItsBagelBot/pkg/codec"
)

var pngBytes = []byte("png-bytes")

type member struct {
	name string
	data []byte
	mode fs.FileMode
}

type outline struct {
	Name         string
	Files        []string
	Widgets      []string
	Mapped       []string
	Approximated []string
	Unsupported  []string
	Settings     string
}

func archiveOf(t *testing.T, members []member) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, m := range members {
		header := &zip.FileHeader{Name: m.name, Method: zip.Deflate}
		if m.mode != 0 {
			header.SetMode(m.mode)
		}
		w, err := zw.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(m.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}

func run(t *testing.T, members []member) (importer.Plan, error) {
	t.Helper()
	r := archiveOf(t, members)
	return importer.Import(r, r.Size())
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := codec.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func sumOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func labels(items []importer.Item) []string {
	var out []string
	for _, item := range items {
		label := string(item.Subject)
		if item.Reason != "" {
			label += ":" + string(item.Reason)
		}
		out = append(out, label)
	}
	slices.Sort(out)
	return out
}

func outlineOf(t *testing.T, plan importer.Plan) outline {
	t.Helper()
	got := outline{Name: plan.Document.Name}
	for _, f := range plan.Files {
		got.Files = append(got.Files, f.Path)
	}
	for _, w := range plan.Document.Widgets {
		got.Widgets = append(got.Widgets, string(w.Kind)+":"+w.Name)
	}
	if len(plan.Document.Widgets) > 0 {
		got.Settings = string(mustJSON(t, plan.Document.Widgets[0].Settings))
	}
	got.Mapped = labels(plan.Report.Mapped)
	got.Approximated = labels(plan.Report.Approximated)
	got.Unsupported = labels(plan.Report.Unsupported)
	return got
}

func manyMembers(count int) []member {
	members := make([]member, count)
	for i := range members {
		members[i] = member{name: fmt.Sprintf("f%d.png", i), data: pngBytes}
	}
	return members
}

func plainMembers() []member {
	names := []string{
		"red/alert.png", "blue/alert.png", "sounds/ding.mp3", "clips/intro.mp4", "LICENSE.pdf",
		"readme.txt", "logo.svg", "fonts/a.ttf", ".DS_Store", "__MACOSX/x",
	}
	members := make([]member, len(names))
	for i, name := range names {
		members[i] = member{name: name, data: []byte("x")}
	}
	return members
}

func widgetMembers() []member {
	return []member{
		{name: "w/widget.html", data: []byte(`<div>{{title}}</div><script>fetch("/x")</script>`)},
		{name: "w/widget.css", data: []byte(".a{color:red}")},
		{name: "w/widget.js", data: []byte("console.log(1)")},
		{name: "w/logo.png", data: pngBytes},
		{name: "w/data.json", data: []byte(`{"title":"Yo","font":"Fancy Script"}`)},
		{name: "w/fields.json", data: []byte(`{
			"title":{"type":"text","label":"Title","value":"Hi"},
			"color":{"type":"colorpicker","label":"Color","value":"#fff"},
			"size":{"type":"slider","min":1,"max":10,"value":5},
			"on":{"type":"checkbox","value":true},
			"pick":{"type":"dropdown","options":{"a":"A","b":"B"},"value":"a"},
			"logo":{"type":"image-input","value":"logo.png"},
			"font":{"type":"googleFont","value":"Roboto"},
			"odd":{"type":"weird","value":1}}`)},
	}
}

func exportMembers() []member {
	export := `{"name":"Mine","width":1920,"height":1080,"widgets":[
		{"type":"Alert Box","name":"Alerts","x":10,"y":20,"width":500,"height":300,"settings":{"image":"logo.png","delay":3}},
		{"type":"Tips","name":"Tip jar"},
		{"type":"Merch"}]}`
	return []member{{name: "logo.png", data: pngBytes}, {name: "overlay.json", data: []byte(export)}}
}

func sellerMembers(t *testing.T, manifestHash string, withAsset bool) []member {
	t.Helper()
	doc := overlays.Document{
		Version: overlays.Version, Kind: overlays.KindCustom, ID: "overlay-1", Name: "Main", Width: 1920, Height: 1080,
		Widgets: []overlays.Widget{{
			ID: "w1", Name: "Logo", Kind: overlays.WidgetImage, W: 100, H: 100, Opacity: 1,
			Settings: map[string]any{"media": "asset:a1"},
		}},
		Assets: map[string]overlays.Asset{
			"a1": {Type: overlays.AssetPNG, Bytes: int64(len(pngBytes)), W: 10, H: 10, SHA256: sumOf(pngBytes)},
		},
	}
	manifest := map[string]any{
		"format": 1, "name": "Pack", "author": "A", "version": "1.0", "license": "MIT",
		"overlays": []string{"overlays/main.json"},
		"assets":   []map[string]string{{"path": "assets/logo.png", "sha256": manifestHash}},
	}
	members := []member{
		{name: "overlay-pack.json", data: mustJSON(t, manifest)},
		{name: "overlays/main.json", data: mustJSON(t, doc)},
		{name: "LICENSE.txt", data: []byte("MIT")},
		{name: "notes.png", data: pngBytes},
	}
	if withAsset {
		members = append(members, member{name: "assets/logo.png", data: pngBytes})
	}
	return members
}

func TestImportRejects(t *testing.T) {
	cases := []struct {
		name    string
		members []member
		want    importer.Code
	}{
		{"traversal", []member{{name: "../evil.png", data: pngBytes}}, importer.CodeUnsafePath},
		{"nested traversal", []member{{name: "a/../../evil.png", data: pngBytes}}, importer.CodeUnsafePath},
		{"absolute", []member{{name: "/etc/evil.png", data: pngBytes}}, importer.CodeUnsafePath},
		{"backslash", []member{{name: `a\b.png`, data: pngBytes}}, importer.CodeUnsafePath},
		{"symlink", []member{{name: "link.png", data: []byte("/etc/passwd"), mode: fs.ModeSymlink | 0o777}}, importer.CodeSymlink},
		{"nested zip by extension", []member{{name: "inner.zip", data: pngBytes}}, importer.CodeNestedArchive},
		{"nested zip by magic", []member{{name: "disguised.png", data: []byte("PK\x03\x04rest")}}, importer.CodeNestedArchive},
		{"too many entries", manyMembers(overlays.ImportMaxEntries + 1), importer.CodeTooManyEntries},
		{"compression bomb", []member{{name: "zeros.webm", data: make([]byte, 4<<20)}}, importer.CodeBombRatio},
		{"wrong manifest hash", sellerMembers(t, sumOf([]byte("other")), true), importer.CodeBadHash},
		{"manifest asset missing", sellerMembers(t, sumOf(pngBytes), false), importer.CodeMissingAsset},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := run(t, tc.members)
			var got *importer.Error
			if !errors.As(err, &got) || got.Code != tc.want {
				t.Fatalf("got %v, want code %s", err, tc.want)
			}
		})
	}
}

func TestImportAccepts(t *testing.T) {
	cases := []struct {
		name    string
		members []member
		want    outline
	}{
		{
			name:    "plain pack with variants and license files",
			members: plainMembers(),
			want: outline{
				Name:   "Imported overlay",
				Files:  []string{"blue/alert.png", "red/alert.png", "sounds/ding.mp3"},
				Mapped: []string{"blue/alert.png", "red/alert.png", "sounds/ding.mp3"},
				Unsupported: []string{
					"LICENSE.pdf:license_file", "clips/intro.mp4:convert_to_webm", "fonts/a.ttf:convert_to_woff2",
					"logo.svg:rejected_type", "readme.txt:license_file",
				},
			},
		},
		{
			name:    "widget bundle",
			members: widgetMembers(),
			want: outline{
				Name:         "Imported overlay",
				Files:        []string{"w/logo.png", "w/widget.bundle.json"},
				Widgets:      []string{"custom:w"},
				Mapped:       []string{"w", "w/logo.png"},
				Approximated: []string{"w/font:approximated_font", "w/odd:unknown_field_type"},
				Unsupported:  []string{"w:network_call"},
				Settings: `{"bundle":"asset:f2","values":{"color":"#fff","font":"Inter","logo":"asset:f1",` +
					`"odd":1,"on":true,"pick":"a","size":5,"title":"Yo"}}`,
			},
		},
		{
			name:    "overlay export",
			members: exportMembers(),
			want: outline{
				Name:        "Mine",
				Files:       []string{"logo.png"},
				Widgets:     []string{"alertbox:Alerts"},
				Mapped:      []string{"Alerts", "logo.png"},
				Unsupported: []string{"Merch:tips_merch", "Tip jar:tips_merch"},
				Settings:    `{"delay":3,"image":"asset:f1"}`,
			},
		},
		{
			name:    "seller pack",
			members: sellerMembers(t, sumOf(pngBytes), true),
			want: outline{
				Name:        "Main",
				Files:       []string{"assets/logo.png"},
				Widgets:     []string{"image:Logo"},
				Mapped:      []string{"assets/logo.png", "overlays/main.json"},
				Unsupported: []string{"LICENSE.txt:license_file", "notes.png:not_in_manifest"},
				Settings:    `{"media":"asset:f1"}`,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := run(t, tc.members)
			if err != nil {
				t.Fatal(err)
			}
			if got := outlineOf(t, plan); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestRemoteFontCheckMatchesTheHostExactly(t *testing.T) {
	cases := []struct {
		name string
		css  string
		want bool
	}{
		{"google fonts import", "@import url(https://fonts.googleapis.com/css?family=Inter);", true},
		{"gstatic font file", "src: url('//fonts.gstatic.com/s/inter.woff2');", true},
		{"hostile host prefix", "@import url(https://notfonts.googleapis.com/css);", false},
		{"hostile host suffix", "@import url(https://fonts.googleapis.com.evil.example/css);", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := run(t, []member{
				{name: "w/widget.html", data: []byte("<div></div>")},
				{name: "w/widget.css", data: []byte(tc.css)},
				{name: "w/widget.js", data: []byte("console.log(1)")},
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := slices.Contains(labels(plan.Report.Unsupported), "w:remote_font"); got != tc.want {
				t.Fatalf("remote_font flagged = %v, want %v (unsupported %v)", got, tc.want, labels(plan.Report.Unsupported))
			}
		})
	}
}

func TestSellerPackAssetsGetAPlaceholderURL(t *testing.T) {
	plan, err := run(t, sellerMembers(t, sumOf(pngBytes), true))
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Document.Assets["f1"].URL; !strings.HasPrefix(got, "https://cdn.itsbagelbot.com/") {
		t.Fatalf("asset url %q", got)
	}
}

func TestImportedWidgetDocumentValidates(t *testing.T) {
	plan, err := run(t, widgetMembers())
	if err != nil {
		t.Fatal(err)
	}
	doc := plan.Document
	for id, asset := range doc.Assets {
		asset.URL = "https://cdn.itsbagelbot.com/u/1/" + id
		doc.Assets[id] = asset
	}
	logo := doc.Assets["f1"]
	logo.W, logo.H = 10, 10
	doc.Assets["f1"] = logo
	if err := overlays.Validate(doc); err != nil {
		t.Fatal(err)
	}
}

func describes(f importer.File, data []byte) bool {
	return int64(len(data)) == f.Size && sumOf(data) == f.SHA256
}

func TestImportedFilesOpenWhatTheyDescribe(t *testing.T) {
	plan, err := run(t, widgetMembers())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range plan.Files {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil || !describes(f, data) {
			t.Fatalf("%s: err %v, size %d/%d", f.Path, err, len(data), f.Size)
		}
	}
}
