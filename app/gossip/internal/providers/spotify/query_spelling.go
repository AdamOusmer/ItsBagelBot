// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package spotify

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

type sourceSpan struct{ start, end int }

type queryField string

func (q queryField) unquoted() string {
	return strings.TrimSpace(strings.ReplaceAll(string(q), `"`, ""))
}

type mappedText struct {
	text  string
	spans []sourceSpan
	track bool
}

type mappedBuilder struct {
	text  strings.Builder
	spans []sourceSpan
	track bool
}

func (b *mappedBuilder) append(text string, span sourceSpan) {
	b.text.WriteString(text)
	for range spanBytes(b.track, text) {
		b.spans = append(b.spans, span)
	}
}

func spanBytes(track bool, text string) int {
	if track {
		return len(text)
	}
	return 0
}

func (b *mappedBuilder) mapped() mappedText { return mappedText{b.text.String(), b.spans, b.track} }

func (s mappedText) source(start, end int) sourceSpan {
	if s.spans == nil {
		return sourceSpan{start, end}
	}
	return sourceSpan{s.spans[start].start, s.spans[end-1].end}
}

func (s mappedText) normalize(form norm.Form) mappedText {
	if !s.track {
		return mappedText{text: form.String(s.text)}
	}
	var segments norm.Iter
	segments.InitString(form, s.text)
	out := mappedBuilder{track: s.track}
	var pending []byte
	for !segments.Done() {
		start := segments.Pos()
		pending = append(pending, segments.Next()...)
		end := segments.Pos()
		if start == end {
			continue
		}
		out.append(string(pending), s.source(start, end))
		pending = pending[:0]
	}
	return out.mapped()
}

func (s mappedText) fold() mappedText {
	out := mappedBuilder{track: s.track}
	start := 0
	for _, cap := range keycap.FindAllStringIndex(s.text, -1) {
		s.foldRun(sourceSpan{start, cap[0]}, &out)
		out.append(" ", s.source(cap[0], cap[1]))
		start = cap[1]
	}
	s.foldRun(sourceSpan{start, len(s.text)}, &out)
	return out.mapped()
}

func (s mappedText) foldRun(run sourceSpan, out *mappedBuilder) {
	var fold letterFold
	for start := run.start; start < run.end; {
		r, width := utf8.DecodeRuneInString(s.text[start:])
		end := start + width
		if r = fold.rune(r); r >= 0 {
			out.append(letterFolds.Replace(string(r)), s.source(start, end))
		}
		start = end
	}
}

func (s mappedText) tokenize(lookup bool) mappedText {
	letters := foldedLetters(s.text)
	if !letters.hasWord() {
		return mappedText{}
	}
	out := mappedBuilder{track: s.track}
	start := 0
	for i, r := range letters {
		end := start + utf8.RuneLen(r)
		out.append(letters.tokenAt(wordIndex(i), lookup), s.source(start, end))
		start = end
	}
	return out.mapped()
}

type sourceWords struct {
	words matchPhrase
	spans []sourceSpan
}

func (raw matchText) sourceWords() sourceWords {
	tokens := raw.tokens(true, false)
	var words sourceWords
	end := 0
	for word := range strings.FieldsSeq(tokens.text) {
		start := end + strings.Index(tokens.text[end:], word)
		end = start + len(word)
		words.words = append(words.words, word)
		words.spans = append(words.spans, tokens.source(start, end))
	}
	return words
}

func (s sourceWords) normalized(content wordRange) sourceWords {
	var out sourceWords
	words := content.words(s.words)
	for i := range words {
		span := s.spans[content.start+i]
		for _, word := range words.normalizedToken(wordIndex(i)) {
			out.words = append(out.words, word)
			out.spans = append(out.spans, span)
		}
	}
	withoutArticle := out.words.withoutArticle()
	if len(withoutArticle) < len(out.words) {
		out.spans[1].start = out.spans[0].start
		out.spans = out.spans[1:]
	}
	out.words = withoutArticle
	return out
}

func (s sourceWords) variants() []sourceWords {
	variants := []sourceWords{s.normalized(wordRange{0, len(s.words)})}
	content := s.words.contentRange()
	if content.isTrimmed(len(s.words)) {
		variants = append(variants, s.normalized(content))
	}
	return variants
}

func (s sourceWords) source(words wordRange) sourceSpan {
	return sourceSpan{s.spans[words.start].start, s.spans[words.end-1].end}
}

func (raw matchText) queryText(span sourceSpan) string {
	return strings.Join(strings.Fields(raw[span.start:span.end].tokens(false, true).text), " ")
}

func (raw matchText) tokens(trackSource, lookup bool) mappedText {
	source := mappedText{text: string(raw), track: trackSource}
	return source.normalize(norm.NFKD).fold().normalize(norm.NFC).tokenize(lookup)
}
