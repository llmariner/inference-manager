package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"
)

func TestExtractAuthorizationFromGRPCRequest(t *testing.T) {
	tcs := []struct {
		description string
		ctx         context.Context
		auth        string
		isErr       bool
	}{
		{
			description: "success",
			ctx: metadata.NewIncomingContext(context.Background(),
				map[string][]string{
					"authorization": {"auth"},
				}),
			auth: "auth",
		},
		{
			description: "no header",
			ctx:         context.Background(),
			isErr:       true,
		},
		{
			description: "multiple auths",
			ctx: metadata.NewIncomingContext(context.Background(),
				map[string][]string{
					"authorization": {"a", "b"},
				}),
			isErr: true,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.description, func(t *testing.T) {
			auth, err := extractAuthorizationFromGRPCRequest(tc.ctx)
			if tc.isErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.auth, auth)
		})
	}
}

func TestExtractAndAppendAuthorization(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(),
		map[string][]string{
			"authorization": {"auth"},
		})
	ctx, err := extractAndAppendAuthorization(ctx)
	assert.NoError(t, err)
	md, ok := metadata.FromOutgoingContext(ctx)
	assert.True(t, ok)
	auth, ok := md["authorization"]
	assert.True(t, ok)
	assert.Equal(t, "auth", auth[0])
}
