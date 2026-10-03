package discovery

import (
	"context"
	"fmt"
	"log/slog"

	"helm.sh/helm/v3/pkg/action"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
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
	Discover(ctx context.Context) ([]DiscoveredRelease, error)
}

// KubeHelmDiscoverer discovers Helm releases via the Kubernetes API.
type KubeHelmDiscoverer struct {
	client            kubernetes.Interface
	restConfig        *rest.Config
	namespaces        []string
	excludeNamespaces map[string]bool
}

// NewHelmDiscoverer creates a HelmDiscoverer backed by the Kubernetes API.
func NewHelmDiscoverer(client kubernetes.Interface, restConfig *rest.Config, namespaces, excludeNamespaces []string) HelmDiscoverer {
	excl := make(map[string]bool, len(excludeNamespaces))
	for _, ns := range excludeNamespaces {
		excl[ns] = true
	}
	return &KubeHelmDiscoverer{
		client:            client,
		restConfig:        restConfig,
		namespaces:        namespaces,
		excludeNamespaces: excl,
	}
}

func (d *KubeHelmDiscoverer) Discover(ctx context.Context) ([]DiscoveredRelease, error) {
	namespaces, err := d.resolveNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	var results []DiscoveredRelease
	for _, ns := range namespaces {
		releases, err := d.listReleasesInNamespace(ns)
		if err != nil {
			slog.Warn("failed to list helm releases", "namespace", ns, "error", err)
			continue
		}
		results = append(results, releases...)
	}

	return results, nil
}

func (d *KubeHelmDiscoverer) listReleasesInNamespace(ns string) ([]DiscoveredRelease, error) {
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(
		newRESTClientGetter(d.restConfig, ns),
		ns,
		"secrets",
		func(format string, v ...interface{}) {
			slog.Debug(fmt.Sprintf(format, v...))
		},
	); err != nil {
		return nil, fmt.Errorf("initializing helm action config for namespace %s: %w", ns, err)
	}

	listAction := action.NewList(actionConfig)
	listAction.All = true
	listAction.AllNamespaces = false
	listAction.SetStateMask()

	releases, err := listAction.Run()
	if err != nil {
		return nil, fmt.Errorf("listing helm releases in namespace %s: %w", ns, err)
	}

	var results []DiscoveredRelease
	for _, rel := range releases {
		chartName := ""
		chartVersion := ""
		appVersion := ""
		if rel.Chart != nil && rel.Chart.Metadata != nil {
			chartName = rel.Chart.Metadata.Name
			chartVersion = rel.Chart.Metadata.Version
			appVersion = rel.Chart.Metadata.AppVersion
		}

		results = append(results, DiscoveredRelease{
			Name:         rel.Name,
			Namespace:    rel.Namespace,
			ChartName:    chartName,
			ChartVersion: chartVersion,
			AppVersion:   appVersion,
			Status:       rel.Info.Status.String(),
		})
	}
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
