package engine

import (
	"testing"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/outgress"

	"github.com/stretchr/testify/assert"
)

func TestTranslate(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantType  string
		wantText  string
		wantColor string
		wantTo    string
	}{
		{"no verb unchanged", "just a normal line", outgress.TypeChat, "just a normal line", "", ""},
		{"announce primary", "/announce hello there", outgress.TypeAnnounce, "hello there", "primary", ""},
		{"announce bare verb only", "/announce", outgress.TypeAnnounce, "", "primary", ""},
		{"announce blue", "/announceblue cold news", outgress.TypeAnnounce, "cold news", "blue", ""},
		{"announce green", "/announcegreen go", outgress.TypeAnnounce, "go", "green", ""},
		{"announce orange", "/announceorange warn", outgress.TypeAnnounce, "warn", "orange", ""},
		{"announce purple", "/announcepurple royal", outgress.TypeAnnounce, "royal", "purple", ""},
		{"shoutout strips target and at", "/shoutout @cooldude check them out", outgress.TypeShoutout, "check them out", "", "cooldude"},
		{"shoutout target only", "/shoutout cooldude", outgress.TypeShoutout, "", "", "cooldude"},
		{"me is plain passthrough not stripped", "/me waves", outgress.TypeChat, "/me waves", "", ""},
		{"unknown slash unchanged", "/unknown thing", outgress.TypeChat, "/unknown thing", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := &module.Output{Type: outgress.TypeChat, Text: tt.text}
			Translate(out)
			assert.Equal(t, tt.wantType, out.Type)
			assert.Equal(t, tt.wantText, out.Text)
			assert.Equal(t, tt.wantColor, out.Color)
			assert.Equal(t, tt.wantTo, out.To)
		})
	}
}

func TestIsEmptyAction(t *testing.T) {
	assert.True(t, isEmptyAction(&module.Output{Type: outgress.TypeAnnounce, Text: ""}))
	assert.False(t, isEmptyAction(&module.Output{Type: outgress.TypeAnnounce, Text: "hi"}))
	assert.True(t, isEmptyAction(&module.Output{Type: outgress.TypeShoutout, To: ""}))
	assert.False(t, isEmptyAction(&module.Output{Type: outgress.TypeShoutout, To: "bob"}))
	assert.True(t, isEmptyAction(&module.Output{Type: outgress.TypeChat, Text: ""}))
	assert.False(t, isEmptyAction(&module.Output{Type: outgress.TypeChat, Text: "hi"}))
}
