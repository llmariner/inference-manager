package server

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
)

func TestGetInferenceStatus(t *testing.T) {
	const clusterName = "cluster-name"

	tcs := []struct {
		name           string
		tenantStatuses map[string][]*v1.EngineStatus
		want           *v1.InferenceStatus
	}{
		{
			name: "in-progress task count",
			tenantStatuses: map[string][]*v1.EngineStatus{
				defaultClusterID: {
					{
						EngineId: "engine1",
						Models: []*v1.EngineStatus_Model{
							{
								Id:                  "model1",
								InProgressTaskCount: 1,
								GpuAllocated:        100,
							},
							{
								Id:                  "model2",
								InProgressTaskCount: 2,
								GpuAllocated:        101,
							},
						},
					},
					{
						EngineId: "engine2",
						Models: []*v1.EngineStatus_Model{
							{
								Id:                  "model1",
								InProgressTaskCount: 10,
								GpuAllocated:        100,
							},
							{
								Id:                  "model3",
								InProgressTaskCount: 12,
								GpuAllocated:        102,
							},
						},
					},
				},
			},
			want: &v1.InferenceStatus{
				ClusterStatuses: []*v1.ClusterStatus{
					{
						Id:   defaultClusterID,
						Name: clusterName,
						EngineStatuses: []*v1.EngineStatus{
							{
								EngineId: "engine1",
								Models: []*v1.EngineStatus_Model{
									{
										Id:                  "model1",
										InProgressTaskCount: 1,
										GpuAllocated:        100,
									},
									{
										Id:                  "model2",
										InProgressTaskCount: 2,
										GpuAllocated:        101,
									},
								},
							},
							{
								EngineId: "engine2",
								Models: []*v1.EngineStatus_Model{
									{
										Id:                  "model1",
										InProgressTaskCount: 10,
										GpuAllocated:        100,
									},
									{
										Id:                  "model3",
										InProgressTaskCount: 12,
										GpuAllocated:        102,
									},
								},
							},
						},
						ModelCount:          3,
						InProgressTaskCount: 1 + 2 + 10 + 12,
						GpuAllocated:        100 + 101 + 102,
					},
				},
				TaskStatus: &v1.TaskStatus{
					InProgressTaskCounts: map[string]int32{
						"model1": 1 + 10,
						"model2": 2,
						"model3": 12,
					},
				},
			},
		},
		{
			name:           "empty tenant statuses",
			tenantStatuses: map[string][]*v1.EngineStatus{},
			want: &v1.InferenceStatus{
				TaskStatus: &v1.TaskStatus{},
			},
		},
		{
			name: "one cluster in auth info",
			tenantStatuses: map[string][]*v1.EngineStatus{
				defaultClusterID: {
					{
						EngineId: "engine1",
					},
				},
			},
			want: &v1.InferenceStatus{
				ClusterStatuses: []*v1.ClusterStatus{
					{
						Id:   defaultClusterID,
						Name: clusterName,
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
			s := &IMS{}

			clusterNamesByID := map[string]string{
				defaultClusterID: clusterName,
			}

			got, err := s.getInferenceStatus(tc.tenantStatuses, clusterNamesByID)
			assert.NoError(t, err)
			assert.True(t, proto.Equal(tc.want, got), cmp.Diff(tc.want, got, protocmp.Transform()))
		})
	}
}
