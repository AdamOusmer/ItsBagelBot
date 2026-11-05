// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	TextOpUpper     = "upper"
	TextOpLower     = "lower"
	TextOpTrim      = "trim"
	TextOpLength    = "length"
	TextOpReplace   = "replace"
	TextOpSubstring = "substring"
	TextOpSplitPick = "split_pick"
	TextOpContains  = "contains"
	TextOpURLEncode = "url_encode"

	splitPickRandom = "random"
)

var (
	ErrTextArgument = errors.New("text_error")

	textOps = enum{
		TextOpUpper, TextOpLower, TextOpTrim, TextOpLength, TextOpReplace,
		TextOpSubstring, TextOpSplitPick, TextOpContains, TextOpURLEncode,
	}
)

type TextStepAction struct {
	Op     string `json:"op"`
	Input  string `json:"input"`
	Arg1   string `json:"arg1"`
	Arg2   string `json:"arg2"`
	SaveAs string `json:"save_as"`
}

func checkTextStep(c chk, p TextStepAction) {
	c.oneOf("op", p.Op, textOps)
	c.text("input", p.Input, upTo(MaxTemplateLength))
	c.text("arg1", p.Arg1, upTo(MaxTemplateLength))
	c.text("arg2", p.Arg2, upTo(MaxTemplateLength))
	c.variableName("save_as", p.SaveAs)
}

// TextOperands are rendered values; any of them may hold viewer input.
type TextOperands struct {
	Input string
	Arg1  string
	Arg2  string
}

func (o TextOperands) capped() TextOperands {
	return TextOperands{
		Input: TruncateRunes(o.Input, MaxTextOperandRunes),
		Arg1:  TruncateRunes(o.Arg1, MaxTextOperandRunes),
		Arg2:  TruncateRunes(o.Arg2, MaxTextOperandRunes),
	}
}

// ApplyText runs one text op. intN returns a value in [0, n) and is only called by a random split_pick.
func ApplyText(op string, in TextOperands, intN func(int) int) (string, error) {
	if op == TextOpLength {
		return strconv.Itoa(utf8.RuneCountInString(in.Input)), nil
	}
	in = in.capped()
	switch op {
	case TextOpSubstring:
		return substringRunes(in)
	case TextOpSplitPick:
		return splitPick(in, intN)
	}
	return applySimpleText(op, in)
}

func applySimpleText(op string, in TextOperands) (string, error) {
	switch op {
	case TextOpUpper:
		return strings.ToUpper(in.Input), nil
	case TextOpLower:
		return strings.ToLower(in.Input), nil
	case TextOpTrim:
		return strings.TrimSpace(in.Input), nil
	case TextOpReplace:
		return replaceAll(in), nil
	case TextOpContains:
		return strconv.FormatBool(ComparePasses(CompareContains, in.Input, in.Arg1)), nil
	case TextOpURLEncode:
		return url.QueryEscape(in.Input), nil
	}
	return "", ErrTextArgument
}

func replaceAll(in TextOperands) string {
	if in.Arg1 == "" {
		return in.Input
	}
	return strings.ReplaceAll(in.Input, in.Arg1, in.Arg2)
}

// substring start is 0-based; split_pick is the 1-based one. An empty length means to the end.
func substringRunes(in TextOperands) (string, error) {
	start, startOK := optionalInt(in.Arg1, 0)
	length, lengthOK := optionalInt(in.Arg2, MaxTextOperandRunes)
	if !startOK || !lengthOK {
		return "", ErrTextArgument
	}
	runes := []rune(in.Input)
	from := min(max(start, 0), int64(len(runes)))
	count := min(max(length, 0), int64(len(runes))-from)
	return string(runes[from : from+count]), nil
}

func optionalInt(raw string, fallback int64) (int64, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback, true
	}
	value, err := strconv.ParseInt(trimmed, 10, 64)
	return value, err == nil
}

// An empty separator splits between runes.
func splitPick(in TextOperands, intN func(int) int) (string, error) {
	parts := strings.Split(in.Input, in.Arg1)
	position, ok := pickPosition(in.Arg2, len(parts), intN)
	if !ok {
		return "", ErrTextArgument
	}
	if position < 0 || position >= int64(len(parts)) {
		return "", nil
	}
	return parts[position], nil
}

func pickPosition(raw string, parts int, intN func(int) int) (int64, bool) {
	trimmed := strings.TrimSpace(raw)
	if strings.EqualFold(trimmed, splitPickRandom) {
		return int64(intN(parts)), true
	}
	position, err := strconv.ParseInt(trimmed, 10, 64)
	return position - 1, err == nil
}
