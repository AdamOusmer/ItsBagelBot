// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"fmt"
	"strings"

	"ItsBagelBot/internal/domain/event/data"
)

func ValidCounterName(name string) (string, error) {
	n := normalizeName(name)
	if n == "" || len(n) > maxCounterName {
		return "", fmt.Errorf("%w: counter name", ErrInvalidInput)
	}
	if strings.Contains(n, ":") {
		return "", fmt.Errorf("%w: counter name", ErrInvalidInput)
	}
	return n, nil
}

func writableCounterName(userID uint64, name string) (string, error) {
	n, err := ValidCounterName(name)
	if err != nil {
		return "", err
	}
	if userID != 0 && data.SystemCounter(n) {
		return "", fmt.Errorf("%w: reserved counter name", ErrInvalidInput)
	}
	return n, nil
}

func ValidScope(scope string) (string, error) {
	switch scope {
	case "", data.CounterScopeChannel:
		return data.CounterScopeChannel, nil
	case data.CounterScopeBot, data.CounterScopeViewer, data.CounterScopeCommand, data.CounterScopeViewerCommand:
		return scope, nil
	default:
		return "", fmt.Errorf("%w: scope", ErrInvalidInput)
	}
}

func entryScoped(scope string) bool {
	switch scope {
	case data.CounterScopeViewer, data.CounterScopeCommand, data.CounterScopeViewerCommand:
		return true
	default:
		return false
	}
}

func bucketed(scope string) bool {
	return scope == data.CounterScopeCommand || scope == data.CounterScopeViewerCommand
}
