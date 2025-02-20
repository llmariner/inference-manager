package server

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/llmariner/rbac-manager/pkg/auth"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
)

func TestGetInferenceStatus(t *testing.T) {
	tcs := []struct {
		name           string
		ctx            context.Context
		tenantStatuses map[string]map[string][]*v1.EngineStatus
		want           *v1.InferenceStatus
	}{
		{
			name:           "empty tenant statuses",
			ctx:            fakeAuthInto(context.Background()),
			tenantStatuses: map[string]map[string][]*v1.EngineStatus{},
			want: &v1.InferenceStatus{
				TaskStatus: &v1.TaskStatus{},
			},
		},
		{
			name: "one cluster in auth info",
			ctx:  fakeAuthInto(context.Background()),
			tenantStatuses: map[string]map[string][]*v1.EngineStatus{
				defaultTenantID: {
					defaultClusterID: {
						{
							EngineId: "engine1",
						},
					},
				},
			},
			want: &v1.InferenceStatus{
				ClusterStatuses: []*v1.ClusterStatus{
					{
						Id: defaultClusterID,
						EngineStatuses: []*v1.EngineStatus{
							{
								EngineId: "engine1",
							},
						},
					},
				},
				TaskStatus: &v1.TaskStatus{},
			},
		},
		{
			name: "two envs in auth info",
			ctx:  fakeAuthInfoWithTwoEnvs(context.Background()),
			tenantStatuses: map[string]map[string][]*v1.EngineStatus{
				defaultTenantID: {
					defaultClusterID: {
						{
							EngineId: "engine1",
						},
					},
				},
			},
			want: &v1.InferenceStatus{
				ClusterStatuses: []*v1.ClusterStatus{
					{
						Id: defaultClusterID,
						EngineStatuses: []*v1.EngineStatus{
							{
								EngineId: "engine1",
							},
						},
					},
				},
				TaskStatus: &v1.TaskStatus{},
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			s := &ISS{
				tenantStatuses: tc.tenantStatuses,
			}
			got, err := s.GetInferenceStatus(tc.ctx, &v1.GetInferenceStatusRequest{})
			assert.NoError(t, err)
			assert.True(t, proto.Equal(tc.want, got), cmp.Diff(tc.want, got, protocmp.Transform()))
		})
	}
}

func fakeAuthInfoWithTwoEnvs(ctx context.Context) context.Context {
	ctx = auth.AppendUserInfoToContext(ctx, auth.UserInfo{
		OrganizationID: defaultOrganizationID,
		ProjectID:      defaultProjectID,
		AssignedKubernetesEnvs: []auth.AssignedKubernetesEnv{
			{
				ClusterID: defaultClusterID,
				Namespace: defaultNamespace,
			},
			{
				ClusterID: defaultClusterID,
				Namespace: "another",
			},
		},
		TenantID: defaultTenantID,
	})
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer token"))
	return ctx
}
