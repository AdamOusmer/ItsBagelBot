package main

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	r3StreamPrefix     = "R3_SHADOW_"
	r3SubjectPrefix    = "twitch.outgress.bench.r3."
	liveStreamPrefix   = "LIVE_NATS_ACCEPTANCE_"
	fleetStreamPrefix  = "FLEET_700K_"
	benchSubjectPrefix = "twitch.outgress.bench."
)

// validateTemporaryTarget is the destructive-operation guard shared by create,
// topology, and cleanup paths. Production stream names and wildcard subjects
// can never pass this boundary, even when an operator mistypes a flag.
func validateTemporaryTarget(stream, subject string) error {
	if !temporaryStreamName(stream) {
		return fmt.Errorf("refusing non-temporary stream name %q", stream)
	}
	if !temporarySubject(subject) {
		return fmt.Errorf("refusing non-temporary subject %q", subject)
	}
	if strings.HasPrefix(stream, r3StreamPrefix) && !strings.HasPrefix(subject, r3SubjectPrefix) {
		return fmt.Errorf("R3 shadow stream %q requires subject prefix %q", stream, r3SubjectPrefix)
	}
	return nil
}

func validateR3ShadowConfig(cfg config) error {
	if !strings.HasPrefix(cfg.stream, r3StreamPrefix) {
		return nil
	}
	if cfg.replicas != 3 {
		return fmt.Errorf("R3 shadow stream %q requires exactly 3 replicas", cfg.stream)
	}
	if cfg.placementTag != "" {
		return fmt.Errorf("R3 shadow stream %q must be untagged so all three peers participate", cfg.stream)
	}
	if cfg.requiredPeers != 3 {
		return fmt.Errorf("R3 shadow stream %q requires exactly 3 topology peers", cfg.stream)
	}
	return nil
}

func temporaryStreamName(name string) bool {
	if !temporaryStreamLength(name) {
		return false
	}
	if !hasTemporaryStreamPrefix(name) {
		return false
	}
	return temporaryStreamCharacters(name)
}

func temporaryStreamLength(name string) bool {
	return len(name) >= 2 && len(name) <= 128
}

func temporaryStreamCharacters(name string) bool {
	for _, char := range name {
		if !temporaryStreamCharacter(char) {
			return false
		}
	}
	return true
}

func temporaryStreamCharacter(char rune) bool {
	switch char {
	case '_', '-':
		return true
	}
	if unicode.IsUpper(char) {
		return true
	}
	return unicode.IsDigit(char)
}

func hasTemporaryStreamPrefix(name string) bool {
	for _, prefix := range []string{r3StreamPrefix, liveStreamPrefix, fleetStreamPrefix} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func temporarySubject(subject string) bool {
	if !temporarySubjectPrefix(subject) {
		return false
	}
	if strings.ContainsAny(subject, "*> \t\r\n") {
		return false
	}
	return !strings.HasSuffix(subject, ".")
}

func temporarySubjectPrefix(subject string) bool {
	if len(subject) <= len(benchSubjectPrefix) {
		return false
	}
	return strings.HasPrefix(subject, benchSubjectPrefix)
}
