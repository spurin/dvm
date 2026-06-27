// Package oci pulls VM component artifacts from an OCI registry using oras-go.
//
// The spurin/ubuntu-cloudimg artifacts package each component as an image
// manifest whose primary layer is a single raw blob (custom +binary media type)
// annotated with its filename, plus a sha256 checksum sidecar and a provenance
// layer. We resolve the manifest, pick the payload layer, fetch it by digest,
// and store it in the content-addressed cache (which re-verifies the digest).
package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"github.com/spurin/diveinto-lab-cli/internal/cache"
	"github.com/spurin/diveinto-lab-cli/internal/component"
)

// Logger is the minimal logging surface the puller needs.
type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
}

// Puller fetches OCI component artifacts into a cache. It satisfies
// component.OCIFetcher.
type Puller struct {
	cache *cache.Cache
	log   Logger
	arch  string // OCI arch ("amd64"|"arm64") used to pick from multi-arch indexes
}

// New returns a Puller backed by the given cache. arch selects which entry of a
// multi-arch (cross-arch) tag to pull; empty defaults to the host architecture.
func New(c *cache.Cache, log Logger, arch string) *Puller {
	if arch == "" {
		arch = runtime.GOARCH
	}
	return &Puller{cache: c, log: log, arch: arch}
}

// newRepository builds an authenticated (anonymous) remote repository for ref,
// rewriting the Docker Hub web host to its registry endpoint. It returns the
// repository and the tag/digest reference to resolve within it.
func newRepository(ref string) (*remote.Repository, string, error) {
	r, err := registry.ParseReference(ref)
	if err != nil {
		return nil, "", fmt.Errorf("parse reference %q: %w", ref, err)
	}
	if r.Registry == "docker.io" {
		r.Registry = "registry-1.docker.io"
	}
	repo, err := remote.NewRepository(r.String())
	if err != nil {
		return nil, "", fmt.Errorf("open repository %q: %w", ref, err)
	}
	repo.Client = &auth.Client{
		Client: retry.DefaultClient,
		Cache:  auth.NewCache(),
	}
	return repo, r.Reference, nil
}

// Fetch pulls the named component from ref and returns the resolved local file.
func (p *Puller) Fetch(ctx context.Context, name, ref string) (component.Component, error) {
	repo, reference, err := newRepository(ref)
	if err != nil {
		return component.Component{}, err
	}
	if reference == "" {
		return component.Component{}, fmt.Errorf("%s: reference %q has no tag or digest", name, ref)
	}

	manDesc, err := repo.Resolve(ctx, reference)
	if err != nil {
		return component.Component{}, fmt.Errorf("%s: resolve %q: %w", name, ref, err)
	}
	manDesc, err = p.resolvePlatform(ctx, repo, manDesc)
	if err != nil {
		return component.Component{}, fmt.Errorf("%s: %w", name, err)
	}
	man, err := fetchManifest(ctx, repo, manDesc)
	if err != nil {
		return component.Component{}, fmt.Errorf("%s: %w", name, err)
	}

	layer, ok := selectPrimaryLayer(name, man)
	if !ok {
		return component.Component{}, fmt.Errorf("%s: no payload layer found in %q", name, ref)
	}
	digest := layer.Digest.String()
	title := layer.Annotations[ocispec.AnnotationTitle]
	if title == "" {
		title = name
	}
	p.log.Debugf("%s: selected layer %s (%s, %d bytes) title=%q", name, digest, layer.MediaType, layer.Size, title)

	if err := p.verifySidecar(ctx, repo, man, title, digest); err != nil {
		return component.Component{}, fmt.Errorf("%s: %w", name, err)
	}

	if p.cache.Has(digest) {
		path, _ := p.cache.BlobPath(digest)
		p.log.Debugf("%s: cache hit %s", name, digest)
		return component.Component{Name: name, Title: title, Path: path, Digest: digest}, nil
	}

	p.log.Infof("☁️  Pulling %s (%s)...", name, ref)
	rc, err := repo.Fetch(ctx, layer)
	if err != nil {
		return component.Component{}, fmt.Errorf("%s: fetch blob: %w", name, err)
	}
	defer rc.Close()
	path, err := p.cache.PutStream(digest, rc)
	if err != nil {
		return component.Component{}, fmt.Errorf("%s: store blob: %w", name, err)
	}
	return component.Component{Name: name, Title: title, Path: path, Digest: digest}, nil
}

// resolvePlatform descends a multi-arch (cross-arch) image index to the manifest
// matching the puller's architecture. Single-manifest descriptors pass through
// unchanged.
func (p *Puller) resolvePlatform(ctx context.Context, repo *remote.Repository, desc ocispec.Descriptor) (ocispec.Descriptor, error) {
	switch desc.MediaType {
	case ocispec.MediaTypeImageIndex, "application/vnd.docker.distribution.manifest.list.v2+json":
	default:
		return desc, nil
	}
	rc, err := repo.Fetch(ctx, desc)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("fetch index: %w", err)
	}
	defer rc.Close()
	data, err := content.ReadAll(rc, desc)
	if err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("read index: %w", err)
	}
	var idx ocispec.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return ocispec.Descriptor{}, fmt.Errorf("parse index: %w", err)
	}
	sub, ok := selectPlatformManifest(idx, p.arch)
	if !ok {
		return ocispec.Descriptor{}, fmt.Errorf("no %s/linux manifest in multi-arch index", p.arch)
	}
	p.log.Debugf("index: selected %s manifest %s", p.arch, sub.Digest)
	return sub, nil
}

func fetchManifest(ctx context.Context, repo *remote.Repository, desc ocispec.Descriptor) (ocispec.Manifest, error) {
	rc, err := repo.Fetch(ctx, desc)
	if err != nil {
		return ocispec.Manifest{}, fmt.Errorf("fetch manifest: %w", err)
	}
	defer rc.Close()
	data, err := content.ReadAll(rc, desc)
	if err != nil {
		return ocispec.Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var man ocispec.Manifest
	if err := json.Unmarshal(data, &man); err != nil {
		return ocispec.Manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	return man, nil
}

// verifySidecar cross-checks the payload digest against the "<title>.sha256"
// checksum sidecar layer when one is present. Absence is not an error.
func (p *Puller) verifySidecar(ctx context.Context, repo *remote.Repository, man ocispec.Manifest, title, digest string) error {
	want := title + ".sha256"
	for _, l := range man.Layers {
		if l.Annotations[ocispec.AnnotationTitle] != want {
			continue
		}
		rc, err := repo.Fetch(ctx, l)
		if err != nil {
			return nil // best-effort; don't fail the pull on sidecar fetch error
		}
		data, err := content.ReadAll(rc, l)
		rc.Close()
		if err != nil {
			return nil
		}
		sum := sidecarSha256(data)
		if sum == "" {
			return nil // unrecognised format; skip
		}
		if "sha256:"+sum != digest {
			return fmt.Errorf("checksum sidecar mismatch: sidecar=sha256:%s layer=%s", sum, digest)
		}
		p.log.Debugf("sidecar checksum verified for %s", title)
		return nil
	}
	return nil
}
