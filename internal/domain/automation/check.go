// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

type (
	field     string
	path      string
	issueCode string
	quantity  int64
	present   bool
	enum      []string
	texts     []string
)

type whole interface{ ~int | ~int64 }

type itemCheck func(field, string)

type span struct{ lo, hi quantity }

func between(lo, hi quantity) span { return span{lo: lo, hi: hi} }

func upTo(hi quantity) span { return span{hi: hi} }

func nonEmpty(hi quantity) span { return span{lo: 1, hi: hi} }

type chk struct {
	out     *[]Issue
	path    path
	trigger string
	perm    int
	depth   int
}

func joinPath(base path, f field) path {
	switch {
	case f == "":
		return base
	case base == "":
		return path(f)
	}
	return base + "." + path(f)
}

func indexed(f field, i index) field { return field(fmt.Sprintf("%s[%d]", f, i)) }

type index int

func (c chk) add(f field, code issueCode) {
	*c.out = append(*c.out, Issue{Path: string(joinPath(c.path, f)), Code: string(code)})
}

func (c chk) sub(f field) chk {
	c.path = joinPath(c.path, f)
	return c
}

func (c chk) elem(i index) chk {
	c.path = path(indexed(field(c.path), i))
	return c
}

func (c chk) nest() chk {
	c.depth++
	return c
}

func (c chk) bound(f field, n quantity, s span, over issueCode) bool {
	switch {
	case n == 0 && s.lo > 0:
		c.add(f, CodeRequired)
	case n < s.lo:
		c.add(f, CodeTooShort)
	case n > s.hi:
		c.add(f, over)
	default:
		return true
	}
	return false
}

func (c chk) text(f field, s string, w span) bool {
	return c.bound(f, quantity(utf8.RuneCountInString(s)), w, CodeTooLong)
}

func (c chk) count(f field, n quantity, w span) bool { return c.bound(f, n, w, CodeTooMany) }

func (c chk) template(f field, s string) bool { return c.text(f, s, nonEmpty(MaxTemplateLength)) }

func inRange[T whole](c chk, f field, n T, w span) {
	if quantity(n) < w.lo || quantity(n) > w.hi {
		c.add(f, CodeOutOfRange)
	}
}

func nonZero[T whole](c chk, f field, n T) {
	if n == 0 {
		c.add(f, CodeOutOfRange)
	}
}

func (c chk) oneOf(f field, s string, set enum) {
	switch {
	case slices.Contains(set, s):
	case s == "":
		c.add(f, CodeRequired)
	default:
		c.add(f, CodeInvalidEnum)
	}
}

func (c chk) format(f field, s string, re *regexp.Regexp) {
	switch {
	case s == "":
		c.add(f, CodeRequired)
	case !re.MatchString(s):
		c.add(f, CodeInvalidFormat)
	}
}

func (c chk) chars(f field, s string, ok func(rune) bool) {
	if strings.IndexFunc(s, func(r rune) bool { return !ok(r) }) >= 0 {
		c.add(f, CodeInvalidFormat)
	}
}

func (c chk) regex(f field, s string, hi quantity) {
	if !c.text(f, s, nonEmpty(hi)) {
		return
	}
	if _, err := regexp.Compile(s); err != nil {
		c.add(f, CodeInvalidRegex)
	}
}

func (c chk) atLeastOne(ok present) {
	if !ok {
		c.add("", CodeRequired)
	}
}

func forEach(f field, list texts, fn itemCheck) {
	for i, s := range list {
		fn(indexed(f, index(i)), s)
	}
}

func (c chk) stringList(f field, list texts, hi quantity, each itemCheck) {
	if c.count(f, quantity(len(list)), upTo(hi)) {
		forEach(f, list, each)
	}
}

func (c chk) eachText(w span) itemCheck {
	return func(f field, s string) { c.text(f, s, w) }
}

func (c chk) eachFormat(re *regexp.Regexp) itemCheck {
	return func(f field, s string) { c.format(f, s, re) }
}

func (c chk) eachOneOf(set enum) itemCheck {
	return func(f field, s string) { c.oneOf(f, s, set) }
}

func isCommandChar(r rune) bool { return !unicode.IsSpace(r) && !unicode.IsControl(r) }

func isNameChar(r rune) bool { return !unicode.IsControl(r) }

func isCounterChar(r rune) bool { return r != ':' && !unicode.IsSpace(r) }
