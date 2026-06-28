package module

import (
	"testing"

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
		{
			name:     "no verb unchanged",
			text:     "just a normal line",
			wantType: outgress.TypeChat,
			wantText: "just a normal line",
		},
		{
			name:      "announce primary",
			text:      "/announce hello there",
			wantType:  outgress.TypeAnnounce,
			wantText:  "hello there",
			wantColor: "primary",
		},
		{
			name:      "announce bare verb only",
			text:      "/announce",
			wantType:  outgress.TypeAnnounce,
			wantText:  "",
			wantColor: "primary",
		},
		{
			name:      "announce blue",
			text:      "/announceblue cold news",
			wantType:  outgress.TypeAnnounce,
			wantText:  "cold news",
			wantColor: "blue",
		},
		{
			name:      "announce green",
			text:      "/announcegreen go",
			wantType:  outgress.TypeAnnounce,
			wantText:  "go",
			wantColor: "green",
		},
		{
			name:      "announce orange",
			text:      "/announceorange warn",
			wantType:  outgress.TypeAnnounce,
			wantText:  "warn",
			wantColor: "orange",
		},
		{
			name:      "announce purple",
			text:      "/announcepurple royal",
			wantType:  outgress.TypeAnnounce,
			wantText:  "royal",
			wantColor: "purple",
		},
		{
			name:     "shoutout strips target and at",
			text:     "/shoutout @cooldude check them out",
			wantType: outgress.TypeShoutout,
			wantText: "check them out",
			wantTo:   "cooldude",
		},
		{
			name:     "shoutout target only",
			text:     "/shoutout cooldude",
			wantType: outgress.TypeShoutout,
			wantText: "",
			wantTo:   "cooldude",
		},
		{
			name:     "me is plain passthrough not stripped",
			text:     "/me waves",
			wantType: outgress.TypeChat,
			wantText: "/me waves",
		},
		{
			name:     "unknown slash unchanged",
			text:     "/unknown thing",
			wantType: outgress.TypeChat,
			wantText: "/unknown thing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := &Output{Type: outgress.TypeChat, Text: tt.text}
			Translate(out)
			assert.Equal(t, tt.wantType, out.Type)
			assert.Equal(t, tt.wantText, out.Text)
			assert.Equal(t, tt.wantColor, out.Color)
			assert.Equal(t, tt.wantTo, out.To)
		})
	}
}

func TestIsEmptyAction(t *testing.T) {
	assert.True(t, isEmptyAction(&Output{Type: outgress.TypeAnnounce, Text: ""}))
	assert.False(t, isEmptyAction(&Output{Type: outgress.TypeAnnounce, Text: "hi"}))
	assert.True(t, isEmptyAction(&Output{Type: outgress.TypeShoutout, To: ""}))
	assert.False(t, isEmptyAction(&Output{Type: outgress.TypeShoutout, To: "bob"}))
	assert.True(t, isEmptyAction(&Output{Type: outgress.TypeChat, Text: ""}))
	assert.False(t, isEmptyAction(&Output{Type: outgress.TypeChat, Text: "hi"}))
}
