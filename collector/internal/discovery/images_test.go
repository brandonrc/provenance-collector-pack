package discovery

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func boolPtr(b bool) *bool { return &b }

func TestImageDiscovery(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitoring"}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nginx-abc123",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind:       "ReplicaSet",
						Name:       "nginx-abc",
						Controller: boolPtr(true),
					},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "nginx", Image: "nginx:1.27-alpine"},
				},
				InitContainers: []corev1.Container{
					{Name: "init", Image: "busybox:1.36"},
				},
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "nginx", ImageID: "docker-pullable://nginx@sha256:abc123"},
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "prometheus-0",
				Namespace: "monitoring",
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind:       "StatefulSet",
						Name:       "prometheus",
						Controller: boolPtr(true),
					},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "prometheus", Image: "prom/prometheus:v2.50.0"},
				},
			},
		},
	)

	discoverer := NewImageDiscoverer(client, nil, nil)
	images, err := discoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(images) != 3 {
		t.Fatalf("expected 3 images, got %d", len(images))
	}

	// Check that we found nginx, busybox init container, and prometheus
	imageNames := make(map[string]bool)
	for _, img := range images {
		imageNames[img.Image] = true
	}

	for _, expected := range []string{"nginx:1.27-alpine", "busybox:1.36", "prom/prometheus:v2.50.0"} {
		if !imageNames[expected] {
			t.Errorf("expected to find image %s", expected)
		}
	}
}

func TestImageDiscoveryExcludeNamespace(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system"}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "default"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "app", Image: "myapp:latest"},
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "kube-dns", Namespace: "kube-system"},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "dns", Image: "coredns:1.11"},
				},
			},
		},
	)

	discoverer := NewImageDiscoverer(client, nil, []string{"kube-system"})
	images, err := discoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(images) != 1 {
		t.Fatalf("expected 1 image (kube-system excluded), got %d", len(images))
	}
	if images[0].Image != "myapp:latest" {
		t.Errorf("expected myapp:latest, got %s", images[0].Image)
	}
}

func TestImageDiscoveryDedup(t *testing.T) {
	// Two pods from the same ReplicaSet with the same image should be deduped
	client := fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nginx-pod-1",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{Kind: "ReplicaSet", Name: "nginx-rs", Controller: boolPtr(true)},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "nginx", Image: "nginx:1.27"},
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "nginx-pod-2",
				Namespace: "default",
				OwnerReferences: []metav1.OwnerReference{
					{Kind: "ReplicaSet", Name: "nginx-rs", Controller: boolPtr(true)},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "nginx", Image: "nginx:1.27"},
				},
			},
		},
	)

	discoverer := NewImageDiscoverer(client, nil, nil)
	images, err := discoverer.Discover(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(images) != 1 {
		t.Fatalf("expected 1 deduplicated image, got %d", len(images))
	}
}

func TestResolveOwner(t *testing.T) {
	tests := []struct {
		name         string
		pod          *corev1.Pod
		expectedKind string
		expectedName string
	}{
		{
			name:         "no owner",
			pod:          &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "standalone"}},
			expectedKind: "Pod",
			expectedName: "standalone",
		},
		{
			name: "replicaset owner",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "ReplicaSet", Name: "nginx-abc", Controller: boolPtr(true)},
					},
				},
			},
			expectedKind: "ReplicaSet",
			expectedName: "nginx-abc",
		},
		{
			name: "controller preferred",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{Kind: "Node", Name: "node-1"},
						{Kind: "DaemonSet", Name: "fluentd", Controller: boolPtr(true)},
					},
				},
			},
			expectedKind: "DaemonSet",
			expectedName: "fluentd",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, name := resolveOwner(tc.pod)
			if kind != tc.expectedKind || name != tc.expectedName {
				t.Errorf("got (%s, %s), want (%s, %s)", kind, name, tc.expectedKind, tc.expectedName)
			}
		})
	}
}
