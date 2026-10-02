// Package kube bindet den Kubernetes-Cluster per WATCH-API an den Store an.
package kube

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// NewClient erzeugt einen Client. Ohne kubeconfig wird die In-Cluster-Konfiguration verwendet,
// mit Fallback auf die Standard-Regeln (KUBECONFIG, ~/.kube/config) für die lokale Entwicklung.
func NewClient(kubeconfig string) (kubernetes.Interface, error) {
	var cfg *rest.Config
	var err error
	if kubeconfig == "" {
		cfg, err = rest.InClusterConfig()
	}
	if kubeconfig != "" || err != nil {
		rules := clientcmd.NewDefaultClientConfigLoadingRules()
		rules.ExplicitPath = kubeconfig
		cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, nil).ClientConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("kubernetes-Konfiguration: %w", err)
	}
	return kubernetes.NewForConfig(cfg)
}
