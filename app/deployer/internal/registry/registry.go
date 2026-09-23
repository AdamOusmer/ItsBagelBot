// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package registry implements ports.Registry against ghcr.io with
// go-containerregistry and the ghcr-pull read credential.
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

// revisionLabel is the config label publish-images stamps on every arch
// image (buildah bud --label org.opencontainers.image.revision=${{ github.sha }}).
const revisionLabel = "org.opencontainers.image.revision"

// required are the platforms every first-party image must carry. The fleet
// mixes Intel and ARM nodes, and a pod scheduled onto an arch its index lacks
// never starts, so a one-arch index is refused before it is pinned.
var required = []string{"linux/amd64", "linux/arm64"}

// Refusal kinds. Every refusal Resolve returns is both a
// *ports.Fail (FailDigestRefused, one plain sentence the console shows as is)
// and one of these kinds, so a stage can return it unchanged while a caller
// that needs the kind still has errors.Is. A tag or repository that does not
// exist carries ports.ErrNotFound.
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

// Config names the image repo and the read credential.
type Config struct {
	Repo     string // "ghcr.io/adamousmer/itsbagelbot"
	Username string
	Password string
	// Transport nil means remote.DefaultTransport.
	Transport http.RoundTripper
}

// Registry implements ports.Registry.
type Registry struct {
	cfg  Config
	auth authn.Authenticator
}

var _ ports.Registry = (*Registry)(nil)

// New builds the registry reader. The credential goes in as basic auth:
// ghcr answers the /v2/ ping with a bearer challenge and go-containerregistry
// trades the basic pair for a pull token, the same exchange `podman login`
// does with the ghcr-pull secret.
func New(cfg Config) *Registry {
	if cfg.Transport == nil {
		cfg.Transport = remote.DefaultTransport
	}
	auth := authn.FromConfig(authn.AuthConfig{Username: cfg.Username, Password: cfg.Password})
	return &Registry{cfg: cfg, auth: auth}
}

// Resolve reads the index a tag points at. It refuses a tag that does not
// exist, a single-platform manifest and an index missing a required
// platform. The revision is reported, not judged: only the stage knows the
// target commit, and the digests stage holds the one revision rule (next to
// the attestation rule it applies to the same pin).
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

// Tags lists every tag of an image, in registry order.
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

// index fetches the manifest a tag points at with remote.Get rather than
// remote.Index: Index fails a single-platform manifest with a media type
// error, and the operator needs to read "not a multi-arch index" instead.
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

// isNotFound reports a 404. The distribution spec answers 404 for both a
// missing tag (MANIFEST_UNKNOWN) and a repository never pushed
// (NAME_UNKNOWN); a 401 or 403 is a credential problem and stays a plain
// error so it is not mistaken for a build that never published.
func isNotFound(err error) bool {
	var terr *transport.Error
	return errors.As(err, &terr) && terr.StatusCode == http.StatusNotFound
}

// platformEntries keeps the index entries that name a platform: podman
// manifest add records one per arch image, and an entry without a platform
// can satisfy neither the arch check nor the revision read.
func platformEntries(m *v1.IndexManifest) []v1.Descriptor {
	var out []v1.Descriptor
	for _, d := range m.Manifests {
		if d.Platform != nil {
			out = append(out, d)
		}
	}
	return out
}

// platformsOf returns sorted, distinct os/arch pairs. The variant is dropped:
// the train requires os/arch, and whether an arm64 entry carries v8 depends on
// the buildah that built it, not on what the node can run.
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

// revisionOf reads the revision label from every platform image, not one:
// each arch is pushed by its own matrix job under <tag>-linux<arch> and
// stitched into the index by tag, so one index can pair images from two
// commits, and reading a single arch would vouch for the other unseen.
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
