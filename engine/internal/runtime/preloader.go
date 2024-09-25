package runtime

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	"github.com/llmariner/rbac-manager/pkg/auth"
	"golang.org/x/sync/errgroup"
	ctrl "sigs.k8s.io/controller-runtime"
)

// NewPreloader creates a new Preloader.
func NewPreloader(rtManager *Manager, ids []string) *Preloader {
	return &Preloader{
		rtManager:             rtManager,
		ids:                   ids,
		initialDelay:          3 * time.Second,
		preloadingParallelism: 3,
	}
}

// Preloader preloads models.
type Preloader struct {
	rtManager *Manager
	ids       []string

	initialDelay          time.Duration
	preloadingParallelism int

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
		g.Go(func() error { return p.rtManager.PullModel(ctx, id) })
	}
	if err := g.Wait(); err != nil {
		return err
	}
	p.logger.Info("Preloading finished")
	return nil
}

// NeedLeaderElection implements LeaderElectionRunnable and always returns true.
func (p *Preloader) NeedLeaderElection() bool {
	return true
}
