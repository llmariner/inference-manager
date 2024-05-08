package server

import (
	"context"
	"fmt"

	"google.golang.org/grpc/metadata"
)

// extractAuthorizationFromGRPCRequest extracts the authorization metadata from the gRPC request.
func extractAuthorizationFromGRPCRequest(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", fmt.Errorf("retrieve metadata from context")
	}
	auth, ok := md["authorization"]
	if !ok {
		return "", fmt.Errorf("no authorization metadata")
	}
	if len(auth) != 1 {
		return "", fmt.Errorf("unexpected number of values in the Authorization header")
	}
	return auth[0], nil
}

// extractAndAppendAuthorization extracts the authorization metadata to the context
// and append that to the outgoing context.
func extractAndAppendAuthorization(ctx context.Context) (context.Context, error) {
	auth, err := extractAuthorizationFromGRPCRequest(ctx)
	if err != nil {
		return nil, err
	}
	return metadata.AppendToOutgoingContext(ctx, "Authorization", auth), nil
}
