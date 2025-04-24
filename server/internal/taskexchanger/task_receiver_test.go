package taskexchanger

import (
	"testing"

	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/stretchr/testify/assert"
)

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
