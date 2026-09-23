// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"strings"
	"testing"
)

// coordinationBuckets names, per BUS user, the KV buckets it owns. Each bucket
// has exactly one writer: outgress's sending coordination, and the deployer's
// run store and deploy lock.
var coordinationBuckets = map[string][]string{
	"outgress_bus": {"outgress_rate", "outgress_batch", "outgress_pause"},
	"deployer_bus": {"DEPLOY_RUNS"},
}

// coordinationSubjects is the exact $JS.API set a bucket owner holds: create
// or reconcile the bucket, and read keys through direct get. Writes ride
// $KV.<bucket>.>, which TestCoordinationBucketPublishIsolation pins.
func coordinationSubjects(user string) []string {
	buckets := coordinationBuckets[user]
	if len(buckets) == 0 {
		return nil
	}
	subjects := []string{jetStreamAPI + "INFO"}
	for _, bucket := range buckets {
		for _, verb := range []string{"STREAM.INFO.", "STREAM.CREATE.", "STREAM.UPDATE.", "STREAM.MSG.GET.", "DIRECT.GET."} {
			subjects = append(subjects, jetStreamAPI+verb+"KV_"+bucket)
		}
		subjects = append(subjects, jetStreamAPI+"DIRECT.GET.KV_"+bucket+".>")
	}
	return subjects
}

// coordinationWrites maps each bucket's $KV write subject to its one owner.
func coordinationWrites() map[string]string {
	writes := make(map[string]string)
	for owner, buckets := range coordinationBuckets {
		for _, bucket := range buckets {
			writes["$KV."+bucket+".>"] = owner
		}
	}
	return writes
}

func TestCoordinationBucketPublishIsolation(t *testing.T) {
	blocks := (authConfig{body: sourceFile{name: "nats-auth.conf"}.read(t)}).busUserBlocks(t)
	for subject, owner := range coordinationWrites() {
		for user, block := range blocks {
			allowed := strings.Contains(block.body, `"`+subject+`"`)
			if allowed != (user == owner) {
				t.Errorf("%s permission for %s = %v", user, subject, allowed)
			}
		}
	}
}
