package infprocessor

import (
	"testing"

	"github.com/llmariner/inference-manager/server/internal/store"

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
	err = et.deleteEngine("e0")
	assert.NoError(t, err)

	got, err = st.ListEnginesByTenantID("tenant0")
	assert.NoError(t, err)
	assert.Empty(t, got)
}
