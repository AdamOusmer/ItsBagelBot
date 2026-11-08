// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tmpl

import (
	"math/rand/v2"
	"strconv"
	"strings"
	"unicode/utf8"
)

func Dynamic(tok Token) (val string, ok bool) {
	switch tok.Name {
	case dynamicRandom:
		return randomValue(tok)
	case dynamicChoice:
		return choiceValue(tok)
	default:
		return "", false
	}
}

const (
	dynamicRandom = "random"
	dynamicChoice = "choice"

	randomDefaultMax = 100
)

func randomValue(tok Token) (string, bool) {
	if !tok.HasPayload {
		return strconv.Itoa(rand.IntN(randomDefaultMax) + 1), true
	}
	rangeText, separator, hasSeparator := strings.Cut(tok.Payload, ":")
	if hasSeparator && utf8.RuneCountInString(separator) != 1 {
		return "", false
	}
	low, high, ok := rangeBounds(rangeText)
	if !ok {
		return "", false
	}
	value := strconv.Itoa(rand.IntN(high-low+1) + low)
	if hasSeparator {
		for i := len(value) - 3; i > 0; i -= 3 {
			value = value[:i] + separator + value[i:]
		}
	}
	return value, true
}

func rangeBounds(payload string) (low, high int, ok bool) {
	lowText, highText, found := strings.Cut(payload, "-")
	if !found {
		return 0, 0, false
	}
	low, err := strconv.Atoi(lowText)
	if err != nil {
		return 0, 0, false
	}
	high, err = strconv.Atoi(highText)
	if err != nil {
		return 0, 0, false
	}
	if high < low {
		return 0, 0, false
	}
	return low, high, true
}

func choiceValue(tok Token) (string, bool) {
	if !tok.HasPayload {
		return "", false
	}
	options := strings.Split(tok.Payload, ",")
	return options[rand.IntN(len(options))], true
}

func NormalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), "!")))
}
