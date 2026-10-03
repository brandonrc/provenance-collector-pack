package discovery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// storeRelease writes a release the way `helm install/upgrade` does with the
// default secrets driver.
func storeRelease(t *testing.T, client *fake.Clientset, ns, name string, version int, chartVersion string, status release.Status) {
	t.Helper()
	rel := &release.Release{
		Name:      name,
		Namespace: ns,
		Version:   version,
		Info:      &release.Info{Status: status},
		Chart: &chart.Chart{Metadata: &chart.Metadata{
			Name: name + "-chart", Version: chartVersion, AppVersion: "app-" + chartVersion,
		}},
	}
	key := fmt.Sprintf("sh.helm.release.v1.%s.v%d", name, version)
	if err := driver.NewSecrets(client.CoreV1().Secrets(ns)).Create(key, rel); err != nil {
		t.Fatalf("storing release %s: %v", key, err)
	}
}

func namespaces(names ...string) []runtime.Object {
	out := make([]runtime.Object, 0, len(names))
	for _, n := range names {
		out = append(out, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: n}})
	}
	return out
}

func TestHelmDiscoverLatestRevisionPerRelease(t *testing.T) {
	client := fake.NewClientset(namespaces("apps", "monitoring", "kube-system")...)
	storeRelease(t, client, "apps", "web", 1, "0.1.0", release.StatusSuperseded)
	storeRelease(t, client, "apps", "web", 3, "0.3.0", release.StatusFailed)
	storeRelease(t, client, "apps", "web", 2, "0.2.0", release.StatusSuperseded)
	storeRelease(t, client, "apps", "api", 1, "1.0.0", release.StatusDeployed)
	storeRelease(t, client, "monitoring", "prometheus", 7, "25.8.0", release.StatusDeployed)
	storeRelease(t, client, "kube-system", "ignored", 1, "9.9.9", release.StatusDeployed)

	got, err := NewHelmDiscoverer(client, nil, []string{"kube-system"}).Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	want := []DiscoveredRelease{
		{Name: "api", Namespace: "apps", ChartName: "api-chart", ChartVersion: "1.0.0", AppVersion: "app-1.0.0", Status: "deployed"},
		{Name: "web", Namespace: "apps", ChartName: "web-chart", ChartVersion: "0.3.0", AppVersion: "app-0.3.0", Status: "failed"},
		{Name: "prometheus", Namespace: "monitoring", ChartName: "prometheus-chart", ChartVersion: "25.8.0", AppVersion: "app-25.8.0", Status: "deployed"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d releases %+v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("release %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestHelmDiscoverExplicitNamespaces(t *testing.T) {
	client := fake.NewClientset(namespaces("a", "b")...)
	storeRelease(t, client, "a", "one", 1, "1.0.0", release.StatusDeployed)
	storeRelease(t, client, "b", "two", 1, "1.0.0", release.StatusDeployed)

	got, err := NewHelmDiscoverer(client, []string{"a", "b"}, []string{"b"}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "one" {
		t.Errorf("explicit namespaces minus exclusions: got %+v", got)
	}
}

func TestHelmDiscoverEmpty(t *testing.T) {
	got, err := NewHelmDiscoverer(fake.NewClientset(namespaces("a")...), nil, nil).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("no releases should be an empty, non-nil list, got %#v", got)
	}
}

// A namespace whose Secrets cannot be listed is reported, and the other
// namespaces' releases are still returned.
func TestHelmDiscoverPartialFailure(t *testing.T) {
	client := fake.NewClientset(namespaces("apps", "locked")...)
	storeRelease(t, client, "apps", "web", 1, "0.1.0", release.StatusDeployed)
	client.PrependReactor("list", "secrets", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetNamespace() == "locked" {
			return true, nil, errors.New(`secrets is forbidden: cannot list resource "secrets" in namespace "locked"`)
		}
		return false, nil, nil
	})

	got, err := NewHelmDiscoverer(client, nil, nil).Discover(context.Background())
	if len(got) != 1 || got[0].Name != "web" {
		t.Errorf("releases from readable namespaces must survive, got %+v", got)
	}
	var nsErr *NamespaceError
	if !errors.As(err, &nsErr) {
		t.Fatalf("want a NamespaceError, got %v", err)
	}
	if nsErr.Namespace != "locked" || !strings.Contains(err.Error(), "forbidden") {
		t.Errorf("unexpected error: %v", err)
	}
	if errors.Unwrap(nsErr) == nil {
		t.Error("NamespaceError must unwrap to the cause")
	}
}

func TestHelmDiscoverNamespaceListFailure(t *testing.T) {
	client := fake.NewClientset()
	client.PrependReactor("list", "namespaces", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("namespaces is forbidden")
	})
	got, err := NewHelmDiscoverer(client, nil, nil).Discover(context.Background())
	if err == nil || got != nil {
		t.Fatalf("want an error and no releases, got %+v, %v", got, err)
	}
}

func TestImageDiscoverErrors(t *testing.T) {
	client := fake.NewClientset(namespaces("a")...)
	client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("pods is forbidden")
	})
	if _, err := NewImageDiscoverer(client, nil, nil).Discover(context.Background()); err == nil {
		t.Error("a pod listing failure must fail image discovery")
	}

	client = fake.NewClientset()
	client.PrependReactor("list", "namespaces", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("namespaces is forbidden")
	})
	if _, err := NewImageDiscoverer(client, nil, nil).Discover(context.Background()); err == nil {
		t.Error("a namespace listing failure must fail image discovery")
	}
}

func TestImageDiscoverExplicitNamespacesAndImageIDs(t *testing.T) {
	client := fake.NewClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "job-x", Namespace: "batch"},
			Spec: corev1.PodSpec{
				InitContainers: []corev1.Container{{Name: "setup", Image: "busybox:1.36"}},
				Containers:     []corev1.Container{{Name: "main", Image: "app:1"}},
			},
			Status: corev1.PodStatus{
				InitContainerStatuses: []corev1.ContainerStatus{{Name: "setup", ImageID: "docker-pullable://busybox@sha256:aa"}},
				ContainerStatuses:     []corev1.ContainerStatus{{Name: "main", ImageID: "docker-pullable://app@sha256:bb"}},
			},
		},
	)
	got, err := NewImageDiscoverer(client, []string{"batch", "skipped"}, []string{"skipped"}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, d := range got {
		ids[d.Image] = d.ImageID
	}
	if ids["busybox:1.36"] != "docker-pullable://busybox@sha256:aa" || ids["app:1"] != "docker-pullable://app@sha256:bb" {
		t.Errorf("image IDs not resolved from container statuses: %+v", got)
	}
}
