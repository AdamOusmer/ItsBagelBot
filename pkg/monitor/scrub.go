// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package monitor

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/newrelic/go-agent/v3/newrelic"
)

const maxScrubbedMessage = 80

var digitRun = regexp.MustCompile(`\d{4,}`)

func ScrubError(err error) error {
	if err == nil {
		return nil
	}
	head, _, _ := strings.Cut(err.Error(), ":")
	head = digitRun.ReplaceAllString(head, "#")
	if len(head) > maxScrubbedMessage {
		head = head[:maxScrubbedMessage]
	}
	return newrelic.Error{Message: head, Class: fmt.Sprintf("%T", err)}
}

func NoticeError(txn *newrelic.Transaction, err error) {
	if txn != nil && err != nil {
		txn.NoticeError(ScrubError(err))
	}
}

func SpanAttributeExcludes() []string {
	return []string{"request.*", "response.*", newrelic.SpanAttributeHTTPURL}
}
