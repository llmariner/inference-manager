package infprocessor

import (
	"reflect"
	"testing"
	"time"

	"github.com/llmariner/inference-manager/server/internal/store"
	"google.golang.org/protobuf/proto"

	v1 "github.com/llmariner/inference-manager/api/v1"
	"github.com/stretchr/testify/assert"
)

func TestEngineTracker(t *testing.T) {
	st, tearDown := store.NewTest(t)
	defer tearDown()

	et := NewEngineTracker(st, "pod0", "podAddr0")
	es := &v1.EngineStatus{
		EngineId: "e0",
		ModelIds: []string{"m0"},
		Ready:    true,
	}
	err := et.addOrUpdateEngine(es, "tenant0")
	assert.NoError(t, err)

	got, err := st.ListEnginesByTenantID("tenant0")
	assert.NoError(t, err)
	assert.Equal(t, 1, len(got))
	assert.Equal(t, "e0", got[0].EngineID)
	assert.True(t, got[0].IsReady)

	gotIDs, err := et.getEngineIDsForModel("m0", "tenant0")
	assert.NoError(t, err)
	assert.Equal(t, 1, len(gotIDs))
	assert.Equal(t, "e0", gotIDs[0])

	gotIDs, err = et.getEngineIDsForModel("other model", "tenant0")
	assert.NoError(t, err)
	assert.Equal(t, "e0", gotIDs[0])

	_, err = et.getEngineIDsForModel("m0", "other tenant")
	assert.Error(t, err)

	// Update the status.
	es.Ready = false
	err = et.addOrUpdateEngine(es, "tenant0")
	assert.NoError(t, err)

	got, err = st.ListEnginesByTenantID("tenant0")
	assert.NoError(t, err)
	assert.Equal(t, 1, len(got))
	assert.Equal(t, "e0", got[0].EngineID)
	assert.False(t, got[0].IsReady)

	// Delete the status.
	err = et.deleteEngine("e0", "tenant0")
	assert.NoError(t, err)

	got, err = st.ListEnginesByTenantID("tenant0")
	assert.NoError(t, err)
	assert.Empty(t, got)

	_, err = et.getEngineIDsForModel("m0", "tenant0")
	assert.Error(t, err)
}

func TestBuildTenantStatus(t *testing.T) {
	now := time.Now()

	status0 := &v1.EngineStatus{
		EngineId: "e0",
		ModelIds: []string{"m0", "m1"},
	}
	b0, err := proto.Marshal(status0)
	assert.NoError(t, err)

	status1 := &v1.EngineStatus{
		EngineId: "e1",
		ModelIds: []string{"m1"},
	}
	b1, err := proto.Marshal(status1)
	assert.NoError(t, err)

	tcs := []struct {
		name    string
		engines []*store.Engine
		want    map[string][]*store.Engine
	}{
		{
			name:    "no engines",
			engines: []*store.Engine{},
			want:    map[string][]*store.Engine{},
		},
		{
			name: "ready engines",
			engines: []*store.Engine{
				{
					EngineID:            "e0",
					Status:              b0,
					IsReady:             true,
					LastStatusUpdatedAt: now.UnixNano(),
				},
				{
					EngineID:            "e1",
					Status:              b1,
					IsReady:             true,
					LastStatusUpdatedAt: now.UnixNano(),
				},
			},
			want: map[string][]*store.Engine{
				"m0": {
					{
						EngineID:            "e0",
						Status:              b0,
						IsReady:             true,
						LastStatusUpdatedAt: now.UnixNano(),
					},
				},
				"m1": {
					{
						EngineID:            "e0",
						Status:              b0,
						IsReady:             true,
						LastStatusUpdatedAt: now.UnixNano(),
					},
					{
						EngineID:            "e1",
						Status:              b1,
						IsReady:             true,
						LastStatusUpdatedAt: now.UnixNano(),
					},
				},
			},
		},
		{
			name: "ignore unready engines",
			engines: []*store.Engine{
				{
					EngineID:            "e0",
					Status:              b0,
					IsReady:             false,
					LastStatusUpdatedAt: now.UnixNano(),
				},
				{
					EngineID:            "e1",
					Status:              b1,
					IsReady:             true,
					LastStatusUpdatedAt: now.UnixNano(),
				},
			},
			want: map[string][]*store.Engine{
				"m1": {
					{
						EngineID:            "e1",
						Status:              b1,
						IsReady:             true,
						LastStatusUpdatedAt: now.UnixNano(),
					},
				},
			},
		},
		{
			name: "ignore stale engines",
			engines: []*store.Engine{
				{
					EngineID:            "e0",
					Status:              b0,
					IsReady:             true,
					LastStatusUpdatedAt: now.Add(-2 * engineActivePeriod).UnixNano(),
				},
				{
					EngineID:            "e1",
					Status:              b1,
					IsReady:             true,
					LastStatusUpdatedAt: now.UnixNano(),
				},
			},
			want: map[string][]*store.Engine{
				"m1": {
					{
						EngineID:            "e1",
						Status:              b1,
						IsReady:             true,
						LastStatusUpdatedAt: now.UnixNano(),
					},
				},
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildTenantStatus(tc.engines, now)
			assert.NoError(t, err)
			assert.Truef(t, reflect.DeepEqual(tc.want, got.enginesByModelID), "want: %v, got: %v", tc.want, got.enginesByModelID)
		})
	}

}
