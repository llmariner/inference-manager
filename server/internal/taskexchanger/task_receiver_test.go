package taskexchanger

import (
	"testing"

	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/stretchr/testify/assert"
)

func TestNeedStatusUpdateForAllTenants(t *testing.T) {
	tcs := []struct {
		name                           string
		engineStatusesByTenantID       map[string][]*v1.EngineStatus
		cachedEngineStatusesByTenantID map[string]map[string]*v1.EngineStatus
		want                           bool
	}{
		{
			name: "no update",
			engineStatusesByTenantID: map[string][]*v1.EngineStatus{
				"tenant0": {
					{
						EngineId: "e0",
					},
				},
			},
			cachedEngineStatusesByTenantID: map[string]map[string]*v1.EngineStatus{
				"tenant0": {
					"e0": {
						EngineId: "e0",
					},
				},
			},
			want: false,
		},
		{
			name: "new engine",
			engineStatusesByTenantID: map[string][]*v1.EngineStatus{
				"tenant0": {
					{
						EngineId: "e0",
					},
					{
						EngineId: "e1",
					},
				},
			},
			cachedEngineStatusesByTenantID: map[string]map[string]*v1.EngineStatus{
				"tenant0": {
					"e0": {
						EngineId: "e0",
					},
				},
			},
			want: true,
		},
		{
			name: "new tenant",
			engineStatusesByTenantID: map[string][]*v1.EngineStatus{
				"tenant0": {
					{
						EngineId: "e0",
					},
					{
						EngineId: "e1",
					},
				},
			},
			cachedEngineStatusesByTenantID: map[string]map[string]*v1.EngineStatus{},
			want:                           true,
		},
		{
			name: "tenant deleted",
			engineStatusesByTenantID: map[string][]*v1.EngineStatus{
				"tenant0": {
					{
						EngineId: "e0",
					},
				},
			},
			cachedEngineStatusesByTenantID: map[string]map[string]*v1.EngineStatus{
				"tenant0": {
					"e0": {
						EngineId: "e0",
					},
				},
				"tenant1": {
					"e1": {
						EngineId: "e1",
					},
				},
			},
			want: true,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got := needStatusUpdateForAllTenants(tc.engineStatusesByTenantID, tc.cachedEngineStatusesByTenantID)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestNeedStatusUpdate(t *testing.T) {
	tcs := []struct {
		name                 string
		engineStatuses       []*v1.EngineStatus
		cachedEngineStatuses map[string]*v1.EngineStatus
		want                 bool
	}{
		{
			name:                 "empty",
			engineStatuses:       []*v1.EngineStatus{},
			cachedEngineStatuses: map[string]*v1.EngineStatus{},
			want:                 false,
		},
		{
			name: "new engine",
			engineStatuses: []*v1.EngineStatus{
				{
					EngineId: "e0",
				},
			},
			cachedEngineStatuses: map[string]*v1.EngineStatus{},
			want:                 true,
		},
		{
			name:           "deleted",
			engineStatuses: []*v1.EngineStatus{},
			cachedEngineStatuses: map[string]*v1.EngineStatus{
				"e0": {
					EngineId: "e0",
				},
			},
			want: true,
		},
		{
			name: "update",
			engineStatuses: []*v1.EngineStatus{
				{
					EngineId: "e0",
					Models: []*v1.EngineStatus_Model{
						{
							Id: "m0",
						},
					},
				},
			},
			cachedEngineStatuses: map[string]*v1.EngineStatus{
				"e0": {
					EngineId: "e0",
					Models: []*v1.EngineStatus_Model{
						{
							Id: "m1",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "no update",
			engineStatuses: []*v1.EngineStatus{
				{
					EngineId: "e0",
					Models: []*v1.EngineStatus_Model{
						{
							Id: "m0",
						},
					},
				},
			},
			cachedEngineStatuses: map[string]*v1.EngineStatus{
				"e0": {
					EngineId: "e0",
					Models: []*v1.EngineStatus_Model{
						{
							Id: "m0",
						},
					},
				},
			},
			want: false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got := needStatusUpdate(tc.engineStatuses, tc.cachedEngineStatuses)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSameEngineStatus(t *testing.T) {
	tcs := []struct {
		name string
		a    *v1.EngineStatus
		b    *v1.EngineStatus
		want bool
	}{

		{
			name: "same",
			a:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c0", Ready: true},
			b:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c0", Ready: true},
			want: true,
		},
		{
			name: "nil",
			a:    nil,
			b:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c0", Ready: true},
			want: false,
		},
		{
			name: "nil models",
			a:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c0", Ready: true, Models: []*v1.EngineStatus_Model{{Id: "m0"}}},
			b:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c0", Ready: true},
			want: false,
		},
		{
			name: "different engine id",
			a:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c0", Ready: true},
			b:    &v1.EngineStatus{EngineId: "e1", ClusterId: "c0", Ready: true},
			want: false,
		},
		{
			name: "different cluster id",
			a:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c0", Ready: true},
			b:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c1", Ready: true},
			want: false,
		},
		{
			name: "different ready status",
			a:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c0", Ready: true},
			b:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c0", Ready: false},
			want: false,
		},
		{
			name: "different model id",
			a:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c0", Ready: true, Models: []*v1.EngineStatus_Model{{Id: "m0"}}},
			b:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c0", Ready: true, Models: []*v1.EngineStatus_Model{{Id: "m1"}}},
			want: false,
		},
		{
			name: "different model count",
			a:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c0", Ready: true, Models: []*v1.EngineStatus_Model{{Id: "m0"}}},
			b:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c0", Ready: true, Models: []*v1.EngineStatus_Model{{Id: "m0"}, {Id: "m1"}}},
			want: false,
		},
		{
			name: "different model ready status",
			a:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c0", Ready: true, Models: []*v1.EngineStatus_Model{{Id: "m0", IsReady: true}}},
			b:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c0", Ready: true, Models: []*v1.EngineStatus_Model{{Id: "m0", IsReady: false}}},
			want: false,
		},
		{
			name: "different model GPU count",
			a:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c0", Ready: true, Models: []*v1.EngineStatus_Model{{Id: "m0", GpuAllocated: 1}}},
			b:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c0", Ready: true, Models: []*v1.EngineStatus_Model{{Id: "m0", GpuAllocated: 2}}},
			want: false,
		},
		{
			name: "different model task count",
			a:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c0", Ready: true, Models: []*v1.EngineStatus_Model{{Id: "m0", InProgressTaskCount: 1}}},
			b:    &v1.EngineStatus{EngineId: "e0", ClusterId: "c0", Ready: true, Models: []*v1.EngineStatus_Model{{Id: "m0", InProgressTaskCount: 2}}},
			want: false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got := sameEngineStatus(tc.a, tc.b)
			assert.Equal(t, tc.want, got)
		})
	}
}
