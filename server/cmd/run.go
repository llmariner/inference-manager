package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/llm-operator/inference-manager/server/internal/config"
	"github.com/llm-operator/inference-manager/server/internal/k8s"
	"github.com/llm-operator/inference-manager/server/internal/monitoring"
	"github.com/llm-operator/inference-manager/server/internal/router"
	"github.com/llm-operator/inference-manager/server/internal/server"
	mv1 "github.com/llm-operator/model-manager/api/v1"
	"github.com/llm-operator/rbac-manager/pkg/auth"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const flagConfig = "config"

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "run",
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := cmd.Flags().GetString(flagConfig)
		if err != nil {
			return err
		}

		c, err := config.Parse(path)
		if err != nil {
			return err
		}

		if err := c.Validate(); err != nil {
			return err
		}

		if err := run(cmd.Context(), &c); err != nil {
			return err
		}
		return nil
	},
}

func run(ctx context.Context, c *config.Config) error {
	errCh := make(chan error)

	mux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			// Do not use the camel case for JSON fields to follow OpenAI API.
			MarshalOptions: protojson.MarshalOptions{
				UseProtoNames:     true,
				EmitDefaultValues: true,
			},
			UnmarshalOptions: protojson.UnmarshalOptions{
				DiscardUnknown: true,
			},
		}),
		runtime.WithIncomingHeaderMatcher(auth.HeaderMatcher),
	)
	// TODO(kenji): Call v1.RegisterChatServiceHandlerFromEndpoint once the gRPC method is defined
	// with gRPC gateway.

	var mclient server.ModelClient
	if c.Debug.UseNoopModelClient {
		mclient = &server.NoopModelClient{}
	} else {
		conn, err := grpc.Dial(c.ModelManagerServerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return err
		}
		mclient = mv1.NewModelsServiceClient(conn)
	}

	m := monitoring.NewMetricsMonitor()
	defer m.UnregisterAllCollectors()

	var k8sClient *k8s.Client
	if c.Debug.UseFakeKubernetesClient {
		conf := c.InferenceManagerEngineConfig
		k8sClient = &k8s.Client{
			CoreClientset: fake.NewSimpleClientset(
				&apiv1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "inference-manager-engine",
						Namespace: conf.Namespace,
						Labels: map[string]string{
							conf.LabelKey: conf.LabelValue,
						},
					},
					Status: apiv1.PodStatus{
						PodIP: "localhost",
					},
				},
			),
		}
	} else {
		var err error
		k8sClient, err = k8s.NewClient()
		if err != nil {
			return err
		}
	}

	r := router.New(c.InferenceManagerEngineConfig, k8sClient)
	s := server.New(r, m, mclient)

	createFile := runtime.MustPattern(
		runtime.NewPattern(
			1,
			[]int{2, 0, 2, 1, 2, 2},
			[]string{"v1", "chat", "completions"},
			"",
		))
	mux.Handle("POST", createFile, s.CreateChatCompletion)

	go func() {
		log.Printf("Starting HTTP server on port %d", c.HTTPPort)
		errCh <- http.ListenAndServe(fmt.Sprintf(":%d", c.HTTPPort), mux)
	}()

	go func() {
		log.Printf("Starting metrics server on port %d", c.MonitoringPort)
		monitorMux := http.NewServeMux()
		monitorMux.Handle("/metrics", promhttp.Handler())
		errCh <- http.ListenAndServe(fmt.Sprintf(":%d", c.MonitoringPort), monitorMux)
	}()

	go func() {
		errCh <- s.Run(ctx, c.GRPCPort, c.AuthConfig)
	}()

	go func() {
		errCh <- r.Run(ctx, errCh)
	}()

	return <-errCh
}

func init() {
	runCmd.Flags().StringP(flagConfig, "c", "", "Configuration file path")
	_ = runCmd.MarkFlagRequired(flagConfig)
}
