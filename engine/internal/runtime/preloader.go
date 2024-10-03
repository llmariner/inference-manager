package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	mv1 "github.com/llmariner/model-manager/api/v1"
	"github.com/llmariner/rbac-manager/pkg/auth"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	ctrl "sigs.k8s.io/controller-runtime"
)

// NewPreloader creates a new Preloader.
func NewPreloader(rtManager *Manager, ids []string, modelClient modelGetter) *Preloader {
	return &Preloader{
		rtManager:             rtManager,
		ids:                   ids,
		initialDelay:          3 * time.Second,
		preloadingParallelism: 3,
		modelClient:           modelClient,
	}
}

// Preloader preloads models.
type Preloader struct {
	rtManager *Manager
	ids       []string

	initialDelay          time.Duration
	preloadingParallelism int

	modelClient modelGetter

	logger logr.Logger
}

// SetupWithManager sets up the multi-autoscaler with the Manager.
func (p *Preloader) SetupWithManager(mgr ctrl.Manager) error {
	p.logger = mgr.GetLogger().WithName("preloader")
	return mgr.Add(p)
}

// Start starts the multi-autoscaler.
func (p *Preloader) Start(ctx context.Context) error {
	time.Sleep(p.initialDelay)
	ctx = ctrl.LoggerInto(ctx, p.logger)
	ctx = auth.AppendWorkerAuthorization(ctx)

	p.logger.Info("Starting preloader", "models", len(p.ids))

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(p.preloadingParallelism)
	for _, id := range p.ids {
		g.Go(func() error {
			if err := p.pullModel(ctx, id); err != nil {
				return fmt.Errorf("pull model %s: %s", id, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return fmt.Errorf("preloading: %s", err)
	}
	p.logger.Info("Preloading finished")
	return nil
}

func (p *Preloader) pullModel(ctx context.Context, id string) error {
	// Wait for the model to be available. This is to avoid crash-looping at the initial LLMariner deployment as
	// model loading might be in-progress.
	//
	// TODO(kenji): Somehow report an error when a specified model ID is never available (e.g., mistyped ID).
	log := p.logger.WithValues("modelID", id)
	for {
		_, err := p.modelClient.GetModel(ctx, &mv1.GetModelRequest{Id: id})
		if err == nil {
			break
		}
		if status.Code(err) != codes.NotFound {
			return fmt.Errorf("get model: %s", err)
		}

		log.Info("Model not found, retrying after sleep", "model", id)
		timer := time.NewTimer(10 * time.Second)
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return p.rtManager.PullModel(ctx, id)
}

// NeedLeaderElection implements LeaderElectionRunnable and always returns true.
func (p *Preloader) NeedLeaderElection() bool {
	return true
}
