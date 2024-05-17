package k8s

import (
	"context"
	"fmt"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Client is a wrapper for Kubernetes clientsets.
type Client struct {
	coreClientset kubernetes.Interface
}

// NewClient creates a Client instance.
func NewClient() (*Client, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("get kube config: %w", err)
	}
	coreClientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("new kubernetes clientset %s: %w", config, err)
	}

	c := Client{
		coreClientset: coreClientset,
	}
	return &c, nil
}

// CoreClientset returns the Core clientset.
func (c *Client) CoreClientset() kubernetes.Interface {
	return c.coreClientset
}

// ListPods lists pods.
func (c *Client) ListPods(ctx context.Context, namespace string, labels map[string]string) ([]*apiv1.Pod, error) {
	client := c.coreClientset.CoreV1().Pods(namespace)
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: labels,
	})
	if err != nil {
		return nil, err
	}
	ps, err := client.List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return nil, err
	}

	var pods []*apiv1.Pod
	for _, p := range ps.Items {
		pods = append(pods, &p)
	}
	return pods, nil
}
