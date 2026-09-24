// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package registry

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

const revisionLabel = "org.opencontainers.image.revision"

// required: a pod scheduled onto an arch its index lacks never starts.
var required = []string{"linux/amd64", "linux/arm64"}

var ErrMissingPlatform = errors.New("required platform missing")

type refusal struct {
	kind error
	fail *ports.Fail
}

func (r *refusal) Error() string   { return r.fail.Message }
func (r *refusal) Unwrap() []error { return []error{r.kind, r.fail} }

func refuse(kind error, format string, args ...any) error {
	return &refusal{kind: kind, fail: ports.Failf(deploy.FailDigestRefused, format, args...)}
}

type Config struct {
	Repo      string
	Username  string
	Password  string
	Transport http.RoundTripper
}

type Registry struct {
	cfg  Config
	auth authn.Authenticator
}

var _ ports.Registry = (*Registry)(nil)

func New(cfg Config) *Registry {
	if cfg.Transport == nil {
		cfg.Transport = remote.DefaultTransport
	}
	auth := authn.FromConfig(authn.AuthConfig{Username: cfg.Username, Password: cfg.Password})
	return &Registry{cfg: cfg, auth: auth}
}

func (r *Registry) Resolve(ctx context.Context, ref ports.ImageRef) (ports.ImageInfo, error) {
	idx, digest, err := r.index(ctx, ref)
	if err != nil {
		return ports.ImageInfo{}, err
	}
	manifest, err := idx.IndexManifest()
	if err != nil {
		return ports.ImageInfo{}, fmt.Errorf("registry: read index %s: %w", label(ref), err)
	}
	entries := platformEntries(manifest)
	platforms := platformsOf(entries)
	if err := requirePlatforms(ref, platforms); err != nil {
		return ports.ImageInfo{}, err
	}
	revision, err := revisionOf(idx, entries)
	if err != nil {
		return ports.ImageInfo{}, fmt.Errorf("registry: read config %s: %w", label(ref), err)
	}
	return ports.ImageInfo{Digest: digest, Platforms: platforms, Revision: revision}, nil
}

func (r *Registry) Tags(ctx context.Context, image deploy.ImageName) ([]deploy.Tag, error) {
	repo, err := r.repository(image)
	if err != nil {
		return nil, err
	}
	names, err := remote.List(repo, r.opts(ctx)...)
	if isNotFound(err) {
		return nil, fmt.Errorf("registry: %s: %w", image, ports.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("registry: list %s: %w", image, err)
	}
	tags := make([]deploy.Tag, len(names))
	for i, n := range names {
		tags[i] = deploy.Tag(n)
	}
	return tags, nil
}

func (r *Registry) index(ctx context.Context, ref ports.ImageRef) (v1.ImageIndex, deploy.Digest, error) {
	repo, err := r.repository(ref.Image)
	if err != nil {
		return nil, "", err
	}
	desc, err := remote.Get(repo.Tag(string(ref.Tag)), r.opts(ctx)...)
	if isNotFound(err) {
		return nil, "", refuse(ports.ErrNotFound, "%s is not in the registry.", label(ref))
	}
	if err != nil {
		return nil, "", fmt.Errorf("registry: resolve %s: %w", label(ref), err)
	}
	if !desc.MediaType.IsIndex() {
		return nil, "", refuse(ErrMissingPlatform, "%s is a single-platform image, not a multi-arch index.", label(ref))
	}
	idx, err := desc.ImageIndex()
	return idx, deploy.Digest(desc.Digest.String()), err
}

func (r *Registry) repository(image deploy.ImageName) (name.Repository, error) {
	repo, err := name.NewRepository(r.cfg.Repo + "/" + string(image))
	if err != nil {
		return name.Repository{}, fmt.Errorf("registry: %w: %w", ports.ErrInvalid, err)
	}
	return repo, nil
}

func (r *Registry) opts(ctx context.Context) []remote.Option {
	return []remote.Option{remote.WithContext(ctx), remote.WithAuth(r.auth), remote.WithTransport(r.cfg.Transport)}
}

func isNotFound(err error) bool {
	var terr *transport.Error
	return errors.As(err, &terr) && terr.StatusCode == http.StatusNotFound
}

func platformEntries(m *v1.IndexManifest) []v1.Descriptor {
	var out []v1.Descriptor
	for _, d := range m.Manifests {
		if d.Platform != nil {
			out = append(out, d)
		}
	}
	return out
}

func platformsOf(entries []v1.Descriptor) []string {
	seen := make(map[string]bool, len(entries))
	for _, d := range entries {
		seen[d.Platform.OS+"/"+d.Platform.Architecture] = true
	}
	return slices.Sorted(maps.Keys(seen))
}

func requirePlatforms(ref ports.ImageRef, platforms []string) error {
	var missing []string
	for _, p := range required {
		if !slices.Contains(platforms, p) {
			missing = append(missing, p)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return refuse(ErrMissingPlatform, "%s is missing %v (its index carries %v).", label(ref), missing, platforms)
}

func revisionOf(idx v1.ImageIndex, entries []v1.Descriptor) (deploy.SHA, error) {
	var revision string
	for i, d := range entries {
		got, err := configLabel(idx, d.Digest)
		if err != nil {
			return "", err
		}
		if i > 0 && got != revision {
			return "", nil
		}
		revision = got
	}
	return deploy.SHA(revision), nil
}

func configLabel(idx v1.ImageIndex, digest v1.Hash) (string, error) {
	img, err := idx.Image(digest)
	if err != nil {
		return "", err
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		return "", err
	}
	return cfg.Config.Labels[revisionLabel], nil
}

func label(ref ports.ImageRef) string { return string(ref.Image) + ":" + string(ref.Tag) }
