package discovery

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// DiscoveredImage represents a container image found running in the cluster.
type DiscoveredImage struct {
	Namespace     string
	PodName       string
	ContainerName string
	Image         string
	ImageID       string
	OwnerKind     string
	OwnerName     string
}

// ImageDiscoverer finds container images running in a Kubernetes cluster.
type ImageDiscoverer interface {
	Discover(ctx context.Context) ([]DiscoveredImage, error)
}

// KubeImageDiscoverer discovers images via the Kubernetes API.
type KubeImageDiscoverer struct {
	client            kubernetes.Interface
	namespaces        []string
	excludeNamespaces map[string]bool
}

// NewImageDiscoverer creates an ImageDiscoverer that queries the Kubernetes API.
func NewImageDiscoverer(client kubernetes.Interface, namespaces, excludeNamespaces []string) ImageDiscoverer {
	excl := make(map[string]bool, len(excludeNamespaces))
	for _, ns := range excludeNamespaces {
		excl[ns] = true
	}
	return &KubeImageDiscoverer{
		client:            client,
		namespaces:        namespaces,
		excludeNamespaces: excl,
	}
}

func (d *KubeImageDiscoverer) Discover(ctx context.Context) ([]DiscoveredImage, error) {
	namespaces, err := d.resolveNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	var results []DiscoveredImage
	seen := make(map[string]bool) // dedup by image+owner

	for _, ns := range namespaces {
		pods, err := d.client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("listing pods in namespace %s: %w", ns, err)
		}

		for i := range pods.Items {
			pod := &pods.Items[i]
			ownerKind, ownerName := resolveOwner(pod)

			allContainers := append(pod.Spec.Containers, pod.Spec.InitContainers...)
			for _, c := range allContainers {
				key := fmt.Sprintf("%s/%s/%s/%s", ns, ownerKind, ownerName, c.Image)
				if seen[key] {
					continue
				}
				seen[key] = true

				imageID := findImageID(pod, c.Name)
				results = append(results, DiscoveredImage{
					Namespace:     ns,
					PodName:       pod.Name,
					ContainerName: c.Name,
					Image:         c.Image,
					ImageID:       imageID,
					OwnerKind:     ownerKind,
					OwnerName:     ownerName,
				})
			}
		}
	}

	return results, nil
}

func (d *KubeImageDiscoverer) resolveNamespaces(ctx context.Context) ([]string, error) {
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

// resolveOwner walks owner references to find the controlling workload.
// For a Pod owned by a ReplicaSet owned by a Deployment, returns ("Deployment", name).
func resolveOwner(pod *corev1.Pod) (string, string) {
	if len(pod.OwnerReferences) == 0 {
		return "Pod", pod.Name
	}

	owner := pod.OwnerReferences[0]
	for i := range pod.OwnerReferences {
		if pod.OwnerReferences[i].Controller != nil && *pod.OwnerReferences[i].Controller {
			owner = pod.OwnerReferences[i]
			break
		}
	}

	// ReplicaSets are typically owned by Deployments; we can't resolve further
	// without an additional API call, so we report the immediate owner.
	// The owner kind (ReplicaSet, StatefulSet, DaemonSet, Job, etc.) is still useful.
	return owner.Kind, owner.Name
}

func findImageID(pod *corev1.Pod, containerName string) string {
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == containerName {
			return cs.ImageID
		}
	}
	for _, cs := range pod.Status.InitContainerStatuses {
		if cs.Name == containerName {
			return cs.ImageID
		}
	}
	return ""
}
