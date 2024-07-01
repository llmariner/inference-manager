package processor

import (
	"context"
	"io"
	"log"

	v1 "github.com/llm-operator/inference-manager/api/v1"
	"github.com/llm-operator/rbac-manager/pkg/auth"
)

type modelLister interface {
	ListSyncedModelIDs(ctx context.Context) []string
}

// NewP returns a new processor.
func NewP(
	engineID string,
	client v1.InferenceWorkerServiceClient,
	modelLister modelLister,
) *P {
	return &P{
		engineID:    engineID,
		client:      client,
		modelLister: modelLister,
	}
}

// P processes tasks.
type P struct {
	engineID    string
	client      v1.InferenceWorkerServiceClient
	modelLister modelLister
}

// Run runs the processor.
func (p *P) Run(ctx context.Context) error {
	auth.AppendWorkerAuthorization(ctx)
	stream, err := p.client.ProcessTasks(ctx)
	if err != nil {
		return err
	}

	// Send an initial request for registraion.
	log.Printf("Registering engine: %s\n", p.engineID)
	if err := p.sendEngineStatus(ctx, stream); err != nil {
		return err
	}

	// TODO(kenji): Periodically send engine status.

	for {
		_, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		// TODO(kenji): Process task.
	}

}

func (p *P) sendEngineStatus(ctx context.Context, stream v1.InferenceWorkerService_ProcessTasksClient) error {
	req := &v1.ProcessTasksRequest{
		Message: &v1.ProcessTasksRequest_EngineStatus{
			EngineStatus: &v1.EngineStatus{
				EngineId: p.engineID,
				ModelIds: p.modelLister.ListSyncedModelIDs(ctx),
			},
		},
	}
	if err := stream.Send(req); err != nil {
		return err
	}

	return nil
}
