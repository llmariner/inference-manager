package server

import (
	"context"
	"log"

	"google.golang.org/grpc"
)

func newLoggingInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		// TODO(kenji): Revisit if this is too verbose.
		log.Printf("Received %q request: %+v\n", info.FullMethod, req)
		return handler(ctx, req)
	}
}
