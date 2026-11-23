// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package moderation

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

var (
	emailPattern = regexp.MustCompile(`[a-z0-9._%+\-]+@[a-z0-9\-]+(?:\.[a-z0-9\-]+)*\.[a-z]{2,}`)
	phonePattern = regexp.MustCompile(`\+\d(?:[\s.()\-]*\d){7,}|\d{10,}|\(\d{3}\)\s?\d{3}[\s.\-]\d{4}|\b\d{3}[\s.\-]\d{3}[\s.\-]\d{4}\b`)
	cardPattern  = regexp.MustCompile(`\b\d{4}[ \-]\d{4}[ \-]\d{4}[ \-]\d{1,7}\b|\b\d{4}[ \-]\d{6}[ \-]\d{5}\b`)
	ipv4Pattern  = regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\b`)
	ipv6Pattern  = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{1,4}:){7}[0-9a-f]{1,4}\b|\b(?:[0-9a-f]{1,4}:){1,6}:[0-9a-f]{1,4}(?::[0-9a-f]{1,4}){0,5}\b`)
)

func stripAddresses(text string) string {
	for {
		next := ipv6Pattern.ReplaceAllString(ipv4Pattern.ReplaceAllString(text, ""), "")
		if next == text {
			return text
		}
		text = next
	}
}

func hasPersonalData(text string) bool {
	literal := strings.ToLower(norm.NFKC.String(text))
	return emailPattern.MatchString(literal) || phonePattern.MatchString(literal) || hasCardNumber(literal)
}

func hasCardNumber(text string) bool {
	for _, candidate := range cardPattern.FindAllString(text, -1) {
		if luhnValid(onlyDigits(candidate)) {
			return true
		}
	}
	return false
}

func onlyDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

func luhnDouble(d int) int {
	d *= 2
	return d - 9*(d/10)
}

func luhnValid(digits string) bool {
	sum := 0
	for i := 0; i < len(digits); i++ {
		d := int(digits[len(digits)-1-i] - '0')
		if i%2 == 1 {
			d = luhnDouble(d)
		}
		sum += d
	}
	return sum%10 == 0
}
