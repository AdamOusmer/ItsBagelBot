// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"strings"
	"testing"
)

func outgressCoordinationSubjects() []string {
	subjects := []string{jetStreamAPI + "INFO"}
	for _, bucket := range []string{"outgress_rate", "outgress_batch", "outgress_pause"} {
		for _, verb := range []string{"STREAM.INFO.", "STREAM.CREATE.", "STREAM.UPDATE.", "STREAM.MSG.GET.", "DIRECT.GET."} {
			subjects = append(subjects, jetStreamAPI+verb+"KV_"+bucket)
		}
		subjects = append(subjects, jetStreamAPI+"DIRECT.GET.KV_"+bucket+".>")
	}
	return subjects
}

func TestOutgressCoordinationBucketPublishIsolation(t *testing.T) {
	blocks := (authConfig{body: sourceFile{name: "nats-auth.conf"}.read(t)}).busUserBlocks(t)
	for _, subject := range []string{"$KV.outgress_rate.>", "$KV.outgress_batch.>", "$KV.outgress_pause.>"} {
		for user, block := range blocks {
			allowed := strings.Contains(block.body, `"`+subject+`"`)
			if allowed != (user == "outgress_bus") {
				t.Errorf("%s permission for %s = %v", user, subject, allowed)
			}
		}
	}
}
