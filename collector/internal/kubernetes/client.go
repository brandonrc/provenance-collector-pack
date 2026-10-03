package kubernetes

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// NewClient creates a Kubernetes clientset. It uses in-cluster config when
// kubeconfig is empty, falling back to the provided kubeconfig path.
func NewClient(kubeconfig string) (kubernetes.Interface, error) {
	cfg, err := restConfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("building kubernetes config: %w", err)
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}
	return client, nil
}

// RestConfig returns the rest.Config used to connect to the cluster.
func RestConfig(kubeconfig string) (*rest.Config, error) {
	return restConfig(kubeconfig)
}

func restConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	return rest.InClusterConfig()
}
