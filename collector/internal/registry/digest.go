package registry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// DigestResolver resolves container image references to their SHA256 digests.
type DigestResolver interface {
	Resolve(ctx context.Context, imageRef string) (string, error)
}

// CraneDigestResolver uses go-containerregistry (crane) to resolve digests.
type CraneDigestResolver struct {
	timeout time.Duration
	opts    []crane.Option
	mu      sync.Mutex
	cache   map[string]string
}

// NewDigestResolver creates a DigestResolver using crane.
func NewDigestResolver(timeout time.Duration, opts ...crane.Option) DigestResolver {
	return &CraneDigestResolver{
		timeout: timeout,
		opts:    opts,
		cache:   make(map[string]string),
	}
}

func (r *CraneDigestResolver) Resolve(ctx context.Context, imageRef string) (string, error) {
	r.mu.Lock()
	if d, ok := r.cache[imageRef]; ok {
		r.mu.Unlock()
		return d, nil
	}
	r.mu.Unlock()

	opts := append([]crane.Option{
		crane.WithContext(ctx),
		crane.WithTransport(remote.DefaultTransport),
	}, r.opts...)

	digest, err := crane.Digest(imageRef, opts...)
	if err != nil {
		return "", fmt.Errorf("resolving digest for %s: %w", imageRef, err)
	}

	r.mu.Lock()
	r.cache[imageRef] = digest
	r.mu.Unlock()

	return digest, nil
}
