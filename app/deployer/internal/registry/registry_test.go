// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package registry

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	ggcr "github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

const (
	shaA deploy.SHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaB deploy.SHA = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type credential struct{ user, pass string }

var ghcrPull = credential{user: "bagel", pass: "ghcr-pull-token"}

var (
	amd64 = v1.Platform{OS: "linux", Architecture: "amd64"}
	arm64 = v1.Platform{OS: "linux", Architecture: "arm64", Variant: "v8"}
	both  = []string{"linux/amd64", "linux/arm64"}
)

type outcome struct {
	Info    ports.ImageInfo
	Tags    []deploy.Tag
	Kind    error
	Code    deploy.FailureCode
	Message string
	Err     bool
}

func observe(err error) outcome {
	o := outcome{Err: err != nil}
	for _, kind := range []error{ports.ErrNotFound, ErrMissingPlatform} {
		if errors.Is(err, kind) {
			o.Kind = kind
			break
		}
	}
	if f, ok := ports.AsFail(err); ok {
		o.Code, o.Message = f.Code, f.Message
	}
	return o
}

func refused(kind error, message string) outcome {
	return outcome{Kind: kind, Code: deploy.FailDigestRefused, Message: message, Err: true}
}

func imageRef(image deploy.ImageName, tag deploy.Tag) ports.ImageRef {
	return ports.ImageRef{Image: image, Tag: tag}
}

type fixture struct {
	t    *testing.T
	repo string
}

func newFixture(t *testing.T) *fixture {
	srv := httptest.NewServer(basicAuth(ggcr.New(ggcr.Logger(log.New(io.Discard, "", 0)))))
	t.Cleanup(srv.Close)
	return &fixture{t: t, repo: strings.TrimPrefix(srv.URL, "http://") + "/adamousmer/itsbagelbot"}
}

func basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if ok && (credential{user: user, pass: pass}) == ghcrPull {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="ghcr-test"`)
		w.WriteHeader(http.StatusUnauthorized)
	})
}

func (f *fixture) client(c credential) *Registry {
	return New(Config{Repo: f.repo, Username: c.user, Password: c.pass})
}

func (f *fixture) tag(ref ports.ImageRef) name.Tag {
	tag, err := name.NewTag(f.repo + "/" + label(ref))
	require.NoError(f.t, err)
	return tag
}

func (f *fixture) push() remote.Option {
	return remote.WithAuth(&authn.Basic{Username: ghcrPull.user, Password: ghcrPull.pass})
}

func (f *fixture) image(revision deploy.SHA) v1.Image {
	img, err := random.Image(256, 1)
	require.NoError(f.t, err)
	img, err = mutate.Config(img, v1.Config{Labels: map[string]string{revisionLabel: string(revision)}})
	require.NoError(f.t, err)
	return img
}

type arch struct {
	platform v1.Platform
	revision deploy.SHA
}

func (f *fixture) pushIndex(ref ports.ImageRef, arches ...arch) deploy.Digest {
	var idx v1.ImageIndex = empty.Index
	for _, a := range arches {
		idx = mutate.AppendManifests(idx, mutate.IndexAddendum{
			Add:        f.image(a.revision),
			Descriptor: v1.Descriptor{Platform: &a.platform},
		})
	}
	require.NoError(f.t, remote.WriteIndex(f.tag(ref), idx, f.push()))
	digest, err := idx.Digest()
	require.NoError(f.t, err)
	return deploy.Digest(digest.String())
}

func TestResolve(t *testing.T) {
	f := newFixture(t)
	multi := f.pushIndex(imageRef("sesame", "v1"), arch{amd64, shaA}, arch{arm64, shaA})
	split := f.pushIndex(imageRef("gossip", "v1"), arch{amd64, shaA}, arch{arm64, shaB})
	f.pushIndex(imageRef("outgress", "v1"), arch{amd64, shaA})
	require.NoError(t, remote.Write(f.tag(imageRef("users", "v1")), f.image(shaA), f.push()))

	tests := []struct {
		name string
		cred credential
		ref  ports.ImageRef
		want outcome
	}{
		{"two-platform index", ghcrPull, imageRef("sesame", "v1"),
			outcome{Info: ports.ImageInfo{Digest: multi, Platforms: both, Revision: shaA}}},
		{"platform images from different commits", ghcrPull, imageRef("gossip", "v1"),
			outcome{Info: ports.ImageInfo{Digest: split, Platforms: both}}},
		{"one-platform index", ghcrPull, imageRef("outgress", "v1"), refused(ErrMissingPlatform,
			"outgress:v1 is missing [linux/arm64] (its index carries [linux/amd64]).")},
		{"single image, no index", ghcrPull, imageRef("users", "v1"), refused(ErrMissingPlatform,
			"users:v1 is a single-platform image, not a multi-arch index.")},
		{"missing tag", ghcrPull, imageRef("sesame", "v2"), refused(ports.ErrNotFound,
			"sesame:v2 is not in the registry.")},
		{"missing repository", ghcrPull, imageRef("ghost", "v1"), refused(ports.ErrNotFound,
			"ghost:v1 is not in the registry.")},
		{"rejected credential is not a missing image", credential{user: "bagel", pass: "revoked"},
			imageRef("sesame", "v1"), outcome{Err: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			info, err := f.client(tc.cred).Resolve(t.Context(), tc.ref)
			got := observe(err)
			got.Info = info
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestTags(t *testing.T) {
	f := newFixture(t)
	f.pushIndex(imageRef("sesame", "v1"), arch{amd64, shaA}, arch{arm64, shaA})
	f.pushIndex(imageRef("sesame", "main-1758000000-aaaaaaaaaaaa"), arch{amd64, shaA}, arch{arm64, shaA})

	tests := []struct {
		name  string
		image deploy.ImageName
		want  outcome
	}{
		{"every tag", "sesame", outcome{Tags: []deploy.Tag{"main-1758000000-aaaaaaaaaaaa", "v1"}}},
		{"missing repository", "ghost", outcome{Kind: ports.ErrNotFound, Err: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tags, err := f.client(ghcrPull).Tags(t.Context(), tc.image)
			got := observe(err)
			got.Tags = tags
			assert.Equal(t, tc.want, got)
		})
	}
}
