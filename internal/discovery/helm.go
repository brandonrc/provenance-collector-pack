package discovery

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// DiscoveredRelease represents a Helm release found in the cluster.
type DiscoveredRelease struct {
	Name         string
	Namespace    string
	ChartName    string
	ChartVersion string
	AppVersion   string
	Status       string
}

// HelmDiscoverer finds deployed Helm releases in the cluster.
type HelmDiscoverer interface {
	// Discover returns the releases it could list. A non-nil error with
	// non-nil results means discovery was partial: some namespaces failed
	// (see NamespaceError) but the returned releases are still valid.
	Discover(ctx context.Context) ([]DiscoveredRelease, error)
}

// NamespaceError reports that the Helm releases of one namespace could not be
// listed (typically RBAC: no get/list on Secrets there).
type NamespaceError struct {
	Namespace string
	Err       error
}

func (e *NamespaceError) Error() string {
	return fmt.Sprintf("listing helm releases in namespace %s: %v", e.Namespace, e.Err)
}

func (e *NamespaceError) Unwrap() error { return e.Err }

// KubeHelmDiscoverer discovers Helm releases by reading Helm's release
// Secrets (the default "secrets" storage driver) through the Kubernetes API.
type KubeHelmDiscoverer struct {
	client            kubernetes.Interface
	namespaces        []string
	excludeNamespaces map[string]bool
}

// NewHelmDiscoverer creates a HelmDiscoverer backed by the Kubernetes API.
func NewHelmDiscoverer(client kubernetes.Interface, namespaces, excludeNamespaces []string) HelmDiscoverer {
	excl := make(map[string]bool, len(excludeNamespaces))
	for _, ns := range excludeNamespaces {
		excl[ns] = true
	}
	return &KubeHelmDiscoverer{
		client:            client,
		namespaces:        namespaces,
		excludeNamespaces: excl,
	}
}

func (d *KubeHelmDiscoverer) Discover(ctx context.Context) ([]DiscoveredRelease, error) {
	namespaces, err := d.resolveNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	results := []DiscoveredRelease{}
	var errs []error
	for _, ns := range namespaces {
		releases, err := d.listReleasesInNamespace(ns)
		if err != nil {
			errs = append(errs, &NamespaceError{Namespace: ns, Err: err})
			continue
		}
		results = append(results, releases...)
	}

	return results, errors.Join(errs...)
}

// listReleasesInNamespace returns the latest revision of every release in ns,
// whatever its status, sorted by name. This is what `helm list --all -n ns`
// shows.
func (d *KubeHelmDiscoverer) listReleasesInNamespace(ns string) ([]DiscoveredRelease, error) {
	store := driver.NewSecrets(d.client.CoreV1().Secrets(ns))
	all, err := store.List(func(*release.Release) bool { return true })
	if err != nil {
		return nil, err
	}

	latest := make(map[string]*release.Release, len(all))
	for _, rel := range all {
		if cur, ok := latest[rel.Name]; ok && cur.Version > rel.Version {
			continue
		}
		latest[rel.Name] = rel
	}

	results := make([]DiscoveredRelease, 0, len(latest))
	for _, rel := range latest {
		dr := DiscoveredRelease{Name: rel.Name, Namespace: rel.Namespace}
		if dr.Namespace == "" {
			dr.Namespace = ns
		}
		if rel.Chart != nil && rel.Chart.Metadata != nil {
			dr.ChartName = rel.Chart.Metadata.Name
			dr.ChartVersion = rel.Chart.Metadata.Version
			dr.AppVersion = rel.Chart.Metadata.AppVersion
		}
		if rel.Info != nil {
			dr.Status = rel.Info.Status.String()
		}
		results = append(results, dr)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	return results, nil
}

func (d *KubeHelmDiscoverer) resolveNamespaces(ctx context.Context) ([]string, error) {
	if len(d.namespaces) > 0 {
		var filtered []string
		for _, ns := range d.namespaces {
			if !d.excludeNamespaces[ns] {
				filtered = append(filtered, ns)
			}
		}
		return filtered, nil
	}

	nsList, err := d.client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing namespaces: %w", err)
	}

	var result []string
	for _, ns := range nsList.Items {
		if !d.excludeNamespaces[ns.Name] {
			result = append(result, ns.Name)
		}
	}
	return result, nil
}
