package integrationtest

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/go-logr/logr"
	v1 "github.com/llmariner/inference-manager/api/v1"
	testutl "github.com/llmariner/inference-manager/common/pkg/test"
	"github.com/llmariner/inference-manager/server/internal/config"
	"github.com/llmariner/inference-manager/server/internal/infprocessor"
	"github.com/llmariner/inference-manager/server/internal/router"
	"github.com/llmariner/inference-manager/server/internal/server"
	"github.com/llmariner/inference-manager/server/internal/taskexchanger"
	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TestIntegration tests the integration with two server instances.
// One server instance reiceves a task and routes it to the other server instance.
func TestIntegration(t *testing.T) {
	logger := testutl.NewTestLogger(t)

	var isListeners, wsListeners []net.Listener
	var isPorts, wsPorts []int
	for i := 0; i < 2; i++ {
		l, err := net.Listen("tcp", ":0")
		assert.NoError(t, err)
		port := l.Addr().(*net.TCPAddr).Port
		isListeners = append(isListeners, l)
		isPorts = append(isPorts, port)

		l, err = net.Listen("tcp", ":0")
		assert.NoError(t, err)
		port = l.Addr().(*net.TCPAddr).Port
		wsListeners = append(wsListeners, l)
		wsPorts = append(wsPorts, port)
	}

	var pods []*corev1.Pod
	for i := 0; i < 2; i++ {
		pods = append(pods, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("server:%d", i),
				Namespace: "llmariner",
			},
			Status: corev1.PodStatus{
				PodIP: "localhost",
			},
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	eg, ctx := errgroup.WithContext(ctx)

	var servers []*serverInst
	for i := 0; i < 2; i++ {
		s, err := createServer(
			isPorts[1-i],
			pods[i],
			pods[1-i],
			logger,
		)
		assert.NoError(t, err)
		servers = append(servers, s)

		eg.Go(func() error {
			return s.infProcessor.Run(ctx)
		})
		eg.Go(func() error {
			return s.internalServer.RunWithListener(ctx, isListeners[i])
		})
		eg.Go(func() error {
			return s.wsServer.RunWithListener(ctx, config.AuthConfig{Enable: false}, nil, wsListeners[i])
		})

		// Wait for the gRPC servers become ready.
		cond := func() bool {
			stream, err := newInternalClient(ctx, isPorts[i])
			if err != nil {
				return false
			}
			_ = stream.CloseSend()
			return true
		}
		assert.Eventually(t, cond, 10*time.Second, 100*time.Millisecond, "internal not ready")

		cond = func() bool {
			stream, err := newWSClient(ctx, wsPorts[i])
			if err != nil {
				return false
			}
			_ = stream.CloseSend()
			return true
		}
		assert.Eventually(t, cond, 10*time.Second, 100*time.Millisecond, "ws service not ready")

		// Create a fake engine that connects to the server.
		stream, err := newWSClient(ctx, wsPorts[i])
		assert.NoError(t, err)
		defer func() { _ = stream.CloseSend() }()

		s.fakeEngineClient = stream

		req := &v1.ProcessTasksRequest{
			Message: &v1.ProcessTasksRequest_EngineStatus{
				EngineStatus: &v1.EngineStatus{
					EngineId:   fmt.Sprintf("e%d", i),
					ModelIds:   []string{fmt.Sprintf("m%d", i)},
					SyncStatus: &v1.EngineStatus_SyncStatus{},
					Ready:      true,
				},
			},
		}
		err = stream.Send(req)
		assert.NoError(t, err)

		// Make the task exchanger connect to the remote server.
		_, err = s.taskExchanger.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      pods[1-i].Name,
				Namespace: pods[1-i].Namespace,
			},
		})
		assert.NoError(t, err)
	}

	// Wait for the servers to be connected by the local engine and the remote engine.
	cond := func() bool {
		enginesFound := true
		for _, s := range servers {
			status := s.infProcessor.DumpStatus()
			tenant, ok := status.Tenants["default-tenant-id"]
			if !ok || len(tenant.Engines) < 2 {
				enginesFound = false
				break
			}
		}
		return enginesFound
	}
	assert.Eventually(t, cond, 10*time.Second, 100*time.Millisecond, "engines not found")

	// Create a task. The task uses a model that a remote engine has.
	respCh := make(chan *http.Response)
	eg.Go(func() error {
		resp, err := servers[0].infProcessor.SendChatCompletionTask(
			ctx,
			"default-tenant-id",
			&v1.CreateChatCompletionRequest{
				Model: "m1",
			},
			http.Header{},
		)
		assert.NoError(t, err)
		respCh <- resp
		return nil
	})

	// The fake client that connects to the other server receives the task.
	resp, err := servers[1].fakeEngineClient.Recv()
	assert.NoError(t, err)
	task := resp.NewTask
	assert.Equal(t, "m1", task.Request.GetChatCompletion().Model)

	// Send the task result.
	err = servers[1].fakeEngineClient.Send(&v1.ProcessTasksRequest{
		Message: &v1.ProcessTasksRequest_TaskResult{
			TaskResult: &v1.TaskResult{
				TaskId: task.Id,
				Message: &v1.TaskResult_HttpResponse{
					HttpResponse: &v1.HttpResponse{
						StatusCode: 200,
					},
				},
			},
		},
	})
	assert.NoError(t, err)

	// Receive the HTTP response.
	httpResp := <-respCh
	assert.Equal(t, 200, httpResp.StatusCode)

	cancel()

	for _, s := range servers {
		s.internalServer.Stop()
		s.wsServer.Stop()
	}

	_ = eg.Wait()
}

type serverInst struct {
	infProcessor  *infprocessor.P
	taskExchanger *taskexchanger.E

	internalServer *server.IS
	wsServer       *server.WS

	fakeEngineClient v1.InferenceWorkerService_ProcessTasksClient
}

func createServer(
	otherInternalServerPort int,
	selfPod *corev1.Pod,
	otherPod *corev1.Pod,
	logger logr.Logger,
) (*serverInst, error) {
	ip := infprocessor.NewP(router.New(), logger)

	te := taskexchanger.NewE(
		ip,
		fake.NewFakeClient(otherPod),
		otherInternalServerPort,
		selfPod.Name,
		"labelKey",
		"labelValue",
		logger,
	)

	return &serverInst{
		infProcessor:   ip,
		taskExchanger:  te,
		internalServer: server.NewInternalServer(ip, te, logger),
		wsServer:       server.NewWorkerServiceServer(ip, logger),
	}, nil
}

func newInternalClient(ctx context.Context, port int) (v1.InferenceInternalService_ProcessTasksInternalClient, error) {
	conn, err := grpc.NewClient(fmt.Sprintf("localhost:%d", port), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	isClient := v1.NewInferenceInternalServiceClient(conn)
	return isClient.ProcessTasksInternal(ctx)
}

func newWSClient(ctx context.Context, port int) (v1.InferenceWorkerService_ProcessTasksClient, error) {
	conn, err := grpc.NewClient(fmt.Sprintf("localhost:%d", port), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	wsClient := v1.NewInferenceWorkerServiceClient(conn)
	return wsClient.ProcessTasks(ctx)
}
