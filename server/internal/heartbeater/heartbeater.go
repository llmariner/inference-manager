package heartbeater

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	"github.com/llmariner/inference-manager/server/internal/config"
	"github.com/llmariner/inference-manager/server/internal/infprocessor"
)

// New creates a new heartbeater.
func New(infProcessor *infprocessor.P, c config.EngineHeartbeatConfig, logger logr.Logger) *H {
	return &H{
		infProcessor: infProcessor,
		c:            c,
		logger:       logger.WithName("heartbeater"),
	}
}

// H is a struct that implements the Heartbeater interface.
type H struct {
	infProcessor *infprocessor.P
	c            config.EngineHeartbeatConfig
	logger       logr.Logger
}

// Run runs the heartbeater.
func (h *H) Run(ctx context.Context) error {
	h.logger.Info("Starting heartbeater")

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(h.c.Interval):
			if err := h.infProcessor.SendHeartbeatTaskToEngines(ctx, h.c.Timeout); err != nil {
				h.logger.Error(err, "Failed to send heartbeat")
			}
		}
	}
}
