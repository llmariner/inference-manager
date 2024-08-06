package router

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddOrUpdateEngine(t *testing.T) {
	const (
		tenantID = "tenant0"
	)

	r := New()
	assert.Empty(t, r.mapsByTenantID)

	r.AddOrUpdateEngine("engine0", tenantID, []string{"model1"})
	assert.Len(t, r.mapsByTenantID, 1)
	m, ok := r.mapsByTenantID[tenantID]
	assert.True(t, ok)
	got, ok := m.m[model{id: "model1"}]
	assert.True(t, ok)
	assert.Equal(t, 1, len(got))
	_, ok = got["engine0"]
	assert.True(t, ok)

	assert.Len(t, m.engines, 1)
	_, ok = m.engines["engine0"]
	assert.True(t, ok)
}

func TestGetEngineForModel(t *testing.T) {
	const (
		tenantID = "tenant0"
	)

	r := New()
	r.AddOrUpdateEngine("engine0", tenantID, []string{"model1"})

	engineIDs, err := r.GetEnginesForModel(context.Background(), "model1", tenantID)
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"engine0"}, engineIDs)

	_, err = r.GetEnginesForModel(context.Background(), "model2", tenantID)
	assert.NoError(t, err)
	assert.ElementsMatch(t, []string{"engine0"}, engineIDs)
}
