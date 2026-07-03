package mail

import (
	"strings"
	"testing"
)

func TestGiftHTMLRendersPersonalNote(t *testing.T) {
	html, err := giftHTML("mavey", "happy streaming!", "https://dash.example")
	if err != nil {
		t.Fatalf("giftHTML: %v", err)
	}
	if !strings.Contains(html, "happy streaming!") {
		t.Error("note body missing from html")
	}
	if !strings.Contains(html, "mavey wrote") {
		t.Error("note label missing from html")
	}
}

func TestGiftHTMLEscapesNote(t *testing.T) {
	html, err := giftHTML("", `<script>alert(1)</script>`, "https://dash.example")
	if err != nil {
		t.Fatalf("giftHTML: %v", err)
	}
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Error("note was not HTML-escaped")
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Error("expected escaped note markup")
	}
	// Anonymous gift with a note falls back to the generic label.
	if !strings.Contains(html, "A note for you") {
		t.Error("anonymous note label missing")
	}
}

func TestGiftHTMLOmitsNoteBlockWhenEmpty(t *testing.T) {
	html, err := giftHTML("mavey", "", "https://dash.example")
	if err != nil {
		t.Fatalf("giftHTML: %v", err)
	}
	if strings.Contains(html, "wrote") || strings.Contains(html, "A note for you") {
		t.Error("empty note must not render the note block")
	}
}

func TestGiftTextIncludesNote(t *testing.T) {
	withNote := giftText("mavey", "have fun", "https://dash.example")
	if !strings.Contains(withNote, "have fun") || !strings.Contains(withNote, "mavey wrote") {
		t.Error("plain-text note missing")
	}
	plain := giftText("mavey", "", "https://dash.example")
	if strings.Contains(plain, "wrote") {
		t.Error("empty note must not render in plain text")
	}
}
