package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/go-logr/stdr"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/llmariner/inference-manager/triton-proxy/internal/server"
	"github.com/llmariner/rbac-manager/pkg/auth"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
)

func runCmd() *cobra.Command {
	var (
		port                int
		tritonServerBaseURL string
		logLevel            int
	)
	cmd := &cobra.Command{
		Use:   "run",
		Short: "run",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := run(cmd.Context(), port, tritonServerBaseURL, logLevel); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "HTTP port")
	cmd.Flags().StringVar(&tritonServerBaseURL, "triton-server-base-url", "", "Triton server base URL")
	cmd.Flags().IntVar(&logLevel, "v", 0, "Log level")
	_ = cmd.MarkFlagRequired("port")
	_ = cmd.MarkFlagRequired("triton-server-base-url")
	return cmd
}

func run(ctx context.Context, port int, tritonServerBaseURL string, lv int) error {
	stdr.SetVerbosity(lv)
	logger := stdr.New(log.Default())

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
	srv := server.New(tritonServerBaseURL, logger)

	pat := runtime.MustPattern(
		runtime.NewPattern(
			1,
			[]int{2, 0, 2, 1, 2, 2},
			[]string{"v1", "chat", "completions"},
			"",
		))
	mux.Handle("POST", pat, srv.CreateChatCompletion)

	log := logger.WithName("http")
	log.Info("Starting HTTP server...", "port", port)
	err := http.ListenAndServe(fmt.Sprintf(":%d", port), mux)
	log.Info("Stopped HTTP server")

	return err
}
