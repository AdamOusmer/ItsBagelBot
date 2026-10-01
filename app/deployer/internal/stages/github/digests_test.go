// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

func manifestImages(t *testing.T) []deploy.ImageName {
	t.Helper()
	return sortedImages(pinsByImage(ParsePins(loadManifests(t), testRepo)))
}

func releaseFixture(t *testing.T) (*fixture, map[deploy.ImageName]deploy.ImagePin) {
	t.Helper()
	run := newRun(deploy.KindRelease)
	run.Version, run.Outputs.TagSHA = "v0.2.3-beta", "c1"
	f := newFixture(t, run)
	f.gh.commitMain("c1", loadManifests(t))
	return f, f.publish(manifestImages(t), "v0.2.3-beta", "c1")
}

func TestDigestsReleaseResolvesEveryImage(t *testing.T) {
	f, pins := releaseFixture(t)
	_, err := f.runStage(t, deploy.StageDigests)

	require.NoError(t, err)
	assert.Len(t, pins, 18)
	assert.Equal(t, pins, f.sink.View().Outputs.Digests)
}

func TestDigestsRefusals(t *testing.T) {
	users := ports.ImageRef{Image: "users", Tag: "v0.2.3-beta"}
	cases := []struct {
		name  string
		spoil func(*fixture)
		want  string
	}{
		{"one architecture", func(f *fixture) {
			info := f.reg.images[users]
			info.Platforms = []string{"linux/amd64"}
			f.reg.images[users] = info
		}, "users:v0.2.3-beta: missing linux/arm64"},
		{"built from another commit", func(f *fixture) {
			info := f.reg.images[users]
			info.Revision = "c0"
			f.reg.images[users] = info
		}, `users:v0.2.3-beta: revision "c0", want "c1"`},
		{"no provenance attestation", func(f *fixture) {
			f.gh.attested[f.reg.images[users].Digest] = false
		}, "users:v0.2.3-beta: no build provenance attestation"},
		{"tag missing", func(f *fixture) { delete(f.reg.images, users) }, "users:v0.2.3-beta: tag not found"},
		{"refused by the registry adapter", func(f *fixture) {
			f.reg.refused = map[ports.ImageRef]error{users: ports.Failf(deploy.FailDigestRefused, "users is a single-platform image.")}
		}, "users:v0.2.3-beta: users is a single-platform image."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := releaseFixture(t)
			tc.spoil(f)
			_, err := f.runStage(t, deploy.StageDigests)
			fl, ok := ports.AsFail(err)
			require.True(t, ok, "err = %v, want a refusal", err)
			assert.Equal(t,
				deploy.Failure{Code: deploy.FailDigestRefused, Message: "1 of 18 images refused", LogTail: []string{tc.want}},
				deploy.Failure{Code: fl.Code, Message: fl.Message, LogTail: fl.LogTail})
		})
	}
}

func TestDigestsBump(t *testing.T) {
	const target = "abcdef1234567890ff"
	cases := []struct {
		name     string
		built    []string
		services []string
		want     []deploy.ImageName
		code     deploy.FailureCode
	}{
		{"pins the changed image", []string{"users", "gossip"}, nil, []deploy.ImageName{"users"}, ""},
		{"narrowed to a service", []string{"users", "sesame"}, []string{"sesame"}, []deploy.ImageName{"sesame"}, ""},
		{"nothing changed", []string{"gossip"}, nil, []deploy.ImageName{}, deploy.FailDigestRefused},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := newRun(deploy.KindBump)
			run.TargetSHA, run.Services, run.Outputs.BuildRunID = target, tc.services, 9
			f := newFixture(t, run)
			files := loadManifests(t)
			f.gh.commitMain("c1", files)
			jobs := []ports.Job{job(1, "Select images", succeeded)}
			for i, img := range tc.built {
				jobs = append(jobs, job(int64(i+2), manifestPrefix+img, succeeded))
			}
			f.gh.steps[9] = []runStep{{jobs: jobs}}
			f.reg.tags = map[deploy.ImageName][]deploy.Tag{
				"users":  {"main-100-abcdef123456", "main-200-abcdef123456", "main-300-000000000000", "sha-abcdef123456"},
				"sesame": {"main-200-abcdef123456"},
				"gossip": {"main-150-abcdef123456"},
			}
			f.publish([]deploy.ImageName{"users"}, "main-200-abcdef123456", target)
			f.publish([]deploy.ImageName{"sesame"}, "main-200-abcdef123456", target)
			gossip := pinsByImage(ParsePins(files, testRepo))["gossip"]
			f.reg.images[ports.ImageRef{Image: "gossip", Tag: "main-150-abcdef123456"}] = ports.ImageInfo{Digest: gossip.Digest, Platforms: bothArches, Revision: target}
			f.gh.attested[gossip.Digest] = true

			_, err := f.runStage(t, deploy.StageDigests)
			got := bumpResult{Code: outcome(t, err), Images: sortedImages(f.sink.View().Outputs.Digests)}
			assert.Equal(t, bumpResult{Code: tc.code, Images: tc.want}, got)
		})
	}
}

type bumpResult struct {
	Code   deploy.FailureCode
	Images []deploy.ImageName
}

func TestMainTagPicksTheNewestBuildOfTheCommit(t *testing.T) {
	reg := &fakeRegistry{tags: map[deploy.ImageName][]deploy.Tag{
		"users": {"main-900-000000000000", "main-100-abcdef123456", "main-200-abcdef123456", "main-20-abcdef123456", "latest"},
	}}
	tag, err := mainTag(t.Context(), reg, "users", "abcdef1234567890")
	require.NoError(t, err)
	assert.Equal(t, deploy.Tag("main-200-abcdef123456"), tag)
}
