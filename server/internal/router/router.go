package router

import (
	"context"
	"fmt"
	"log"
	"time"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/inference-manager/server/internal/config"
	"github.com/llm-operator/inference-manager/server/internal/k8s"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"k8s.io/apimachinery/pkg/util/runtime"
)

const tickPeriod = 60 * time.Second

type model struct {
	id string
}

// R manages the routing table of models to engines.
type R struct {
	// m is the route map.
	m             *routeMap
	engineClients map[string]v1.InferenceEngineInternalServiceClient
	k8sClient     *k8s.Client
	imeConfig     config.InferenceManagerEngineConfig
	engineLabels  map[string]string
}

// New creates a new router.
func New(c config.InferenceManagerEngineConfig, k8sClient *k8s.Client) *R {
	labels := map[string]string{
		c.LabelKey: c.LabelValue,
	}
	return &R{
		m:             newRouteMap(),
		engineClients: make(map[string]v1.InferenceEngineInternalServiceClient),
		k8sClient:     k8sClient,
		imeConfig:     c,
		engineLabels:  labels,
	}
}

// Run updates the routes periodically and upon pod events.
func (r *R) Run(ctx context.Context, errCh chan error) error {
	stopper := make(chan struct{})
	defer close(stopper)
	defer runtime.HandleCrash()

	// initialize the routes before starting the informer.
	if err := r.refreshRoutes(ctx); err != nil {
		return err
	}

	is, err := k8s.NewInformers(r.k8sClient, stopper, r.imeConfig.Namespace, r.engineLabels)
	if err != nil {
		return err
	}

	if err := is.SetPodEventHandlers(ctx, []k8s.EventHandlerWithContext{
		newPodEventHandler(r.m, errCh),
	}); err != nil {
		return err
	}

	log.Printf("Starting informer.\n")
	go is.Run()

	// refresh routes periodically, since a engine may evict models.
	ticker := time.NewTicker(tickPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := r.refreshRoutes(ctx); err != nil {
				return err
			}
		case <-stopper:
			return nil
		case err := <-errCh:
			return err
		}
	}
}

// GetEngineForModel returns the engine address (ip and port) for the given model.
func (r *R) GetEngineForModel(ctx context.Context, modelID string) (string, error) {
	routes := r.m.getRoute(model{id: modelID})
	if len(routes) != 0 {
		// TODO(guangrui): handle load balance.
		return fmt.Sprintf("%s:%d", routes[0], r.imeConfig.OllamaPort), nil
	}

	ip, err := r.m.findLeastLoadedEngine()
	if err != nil {
		return "", err
	}

	if err := r.pullModel(ctx, modelID, ip); err != nil {
		return "", err
	}

	r.m.addRoute(model{id: modelID}, ip)
	return fmt.Sprintf("%s:%d", ip, r.imeConfig.OllamaPort), nil
}

func (r *R) refreshRoutes(ctx context.Context) error {
	pods, err := r.k8sClient.ListPods(ctx, r.imeConfig.Namespace, r.engineLabels)
	if err != nil {
		return err
	}
	log.Printf("Refreshing routes, found %d pods\n", len(pods))

	newRM := newRouteMap()
	for _, pod := range pods {
		ip := pod.Status.PodIP
		if ip == "" {
			continue
		}
		models, err := r.listModels(ctx, ip)
		if err != nil {
			// Gracefully handle the error to avoid hard mutual dependency between the server and the engine.
			log.Printf("Failed to list models on %s: %s\n", ip, err)
			continue
		}
		for _, m := range models {
			newRM.addRoute(model{id: m}, ip)
		}
		newRM.addServer(ip)
	}
	newRM.printRoute()

	r.m.replace(newRM)
	return nil
}

func (r *R) listModels(ctx context.Context, ip string) ([]string, error) {
	ec, err := r.getEngineClient(ip)
	if err != nil {
		return nil, err
	}

	resp, err := ec.ListModels(ctx, &v1.ListModelsRequest{})
	if err != nil {
		return nil, err
	}

	var modelIDs []string
	for _, model := range resp.Models {
		modelIDs = append(modelIDs, model.Id)
	}
	return modelIDs, nil
}

// pullModel pulls the model from the engine of the IP.
func (r *R) pullModel(ctx context.Context, modelID, ip string) error {
	ec, err := r.getEngineClient(ip)
	if err != nil {
		return err
	}

	log.Printf("Pulling model %s on %s\n", modelID, ip)
	if _, err = ec.PullModel(ctx, &v1.PullModelRequest{Id: modelID}); err != nil {
		return err
	}
	log.Printf("Finished pulling model %s on %s\n", modelID, ip)

	return nil
}

func (r *R) getEngineClient(ip string) (v1.InferenceEngineInternalServiceClient, error) {
	if ec, ok := r.engineClients[ip]; ok {
		return ec, nil
	}

	dest := fmt.Sprintf("%s:%d", ip, r.imeConfig.InternalGRPCPort)
	conn, err := grpc.Dial(dest, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	r.engineClients[ip] = v1.NewInferenceEngineInternalServiceClient(conn)
	return r.engineClients[ip], nil
}
