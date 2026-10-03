package registry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
)

// DigestResolver resolves container image references to their SHA256 digests.
type DigestResolver interface {
	Resolve(ctx context.Context, imageRef string) (string, error)
}

// CraneDigestResolver uses go-containerregistry (crane) to resolve digests.
type CraneDigestResolver struct {
	timeout time.Duration
	client  *Client
	mu      sync.Mutex
	cache   map[string]string
}

// NewDigestResolver creates a DigestResolver using crane. timeout bounds each
// lookup (0 means no limit beyond ctx); client supplies auth and TLS settings
// (nil means the defaults).
func NewDigestResolver(timeout time.Duration, client *Client) DigestResolver {
	return &CraneDigestResolver{
		timeout: timeout,
		client:  client,
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

	if r.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}

	digest, err := crane.Digest(imageRef, r.client.CraneOptions(ctx, imageRef)...)
	if err != nil {
		return "", fmt.Errorf("resolving digest for %s: %w", imageRef, err)
	}

	r.mu.Lock()
	r.cache[imageRef] = digest
	r.mu.Unlock()

	return digest, nil
}
