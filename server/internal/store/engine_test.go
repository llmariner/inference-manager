package store

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestCreateOrUpdateEngine(t *testing.T) {
	st, tearDown := NewTest(t)
	defer tearDown()

	e0 := &Engine{
		EngineID: "e0",
		TenantID: "t0",
		IsReady:  true,
	}

	err := st.CreateOrUpdateEngine(e0)
	assert.NoError(t, err)

	got, err := st.ListEnginesByTenantID("t0")
	assert.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "e0", got[0].EngineID)
	assert.True(t, got[0].IsReady)

	// Update.
	e0.IsReady = false
	err = st.CreateOrUpdateEngine(e0)
	assert.NoError(t, err)

	got, err = st.ListEnginesByTenantID("t0")
	assert.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "e0", got[0].EngineID)
	assert.False(t, got[0].IsReady)

	// Add more engines.
	e1 := &Engine{
		EngineID: "e1",
		TenantID: "t0",
		IsReady:  true,
	}
	err = st.CreateOrUpdateEngine(e1)
	assert.NoError(t, err)

	e2 := &Engine{
		EngineID: "e2",
		TenantID: "t1",
		IsReady:  true,
	}
	err = st.CreateOrUpdateEngine(e2)
	assert.NoError(t, err)

	got, err = st.ListEnginesByTenantID("t0")
	assert.NoError(t, err)
	assert.Len(t, got, 2)
	assert.ElementsMatch(t, []string{"e0", "e1"}, []string{got[0].EngineID, got[1].EngineID})

	got, err = st.ListEnginesByTenantID("t1")
	assert.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "e2", got[0].EngineID)
}

func TestDeleteEngine(t *testing.T) {
	st, tearDown := NewTest(t)
	defer tearDown()

	e0 := &Engine{
		EngineID: "e0",
		TenantID: "t0",
		IsReady:  true,
	}

	err := st.CreateOrUpdateEngine(e0)
	assert.NoError(t, err)

	err = st.DeleteEngine("e0")
	assert.NoError(t, err)

	got, err := st.ListEnginesByTenantID("t0")
	assert.NoError(t, err)
	assert.Len(t, got, 0)

	// Delete non-existing engine.
	err = st.DeleteEngine("e0")
	assert.True(t, errors.Is(err, gorm.ErrRecordNotFound))
}
