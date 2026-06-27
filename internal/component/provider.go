package component

import (
	"context"
	"fmt"
	"os"
)

// OCIFetcher pulls an OCI component artifact and returns the resolved local file.
// Implemented by internal/oci.
type OCIFetcher interface {
	Fetch(ctx context.Context, name, ref string) (Component, error)
}

// Resolver turns a component name+ref into a usable local Component, dispatching
// to the OCI fetcher or the local filesystem based on the ref kind.
type Resolver struct {
	OCI OCIFetcher
}

// Resolve resolves a single named component reference to a local file.
func (r *Resolver) Resolve(ctx context.Context, name, ref string) (Component, error) {
	if ref == "" {
		return Component{}, fmt.Errorf("%s: empty reference", name)
	}
	parsed := ParseRef(ref)
	switch parsed.Kind {
	case KindLocal:
		abs, err := Abs(parsed.Value)
		if err != nil {
			return Component{}, fmt.Errorf("%s: %w", name, err)
		}
		fi, err := os.Stat(abs)
		if err != nil {
			return Component{}, fmt.Errorf("%s: local path not found: %w", name, err)
		}
		title := fi.Name()
		return Component{Name: name, Title: title, Path: abs}, nil
	case KindOCI:
		if r.OCI == nil {
			return Component{}, fmt.Errorf("%s: OCI reference %q given but no OCI fetcher configured", name, ref)
		}
		return r.OCI.Fetch(ctx, name, parsed.Value)
	default:
		return Component{}, fmt.Errorf("%s: unhandled ref kind", name)
	}
}
