package router

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddGetDeleteRouteAndAddDeleteServer(t *testing.T) {
	r := newRouteMap()
	r.addRoute(model{id: "model1"}, "engine1")
	r.addRoute(model{id: "model2"}, "engine2")

	got := r.getRoute(model{id: "model1"})
	want := "engine1"
	assert.Equal(t, 1, len(got))
	assert.Equal(t, want, got[0])

	r.deleteRoute(model{id: "model1"}, "engine1")

	got = r.getRoute(model{id: "model1"})
	assert.Equal(t, 0, len(got))

	got = r.getRoute(model{id: "model2"})
	want = "engine2"
	assert.Equal(t, 1, len(got))
	assert.Equal(t, want, got[0])

	r.deleteEngine("engine2")
	got = r.getRoute(model{id: "model2"})
	assert.Equal(t, 0, len(got))
}

func TestFindLeastLoadedEngine(t *testing.T) {
	r := newRouteMap()
	r.addRoute(model{id: "model1"}, "engine1")
	r.addOrUpdateEngine("engine2", []string{})
	t.Logf("Dumping the current route map:\n")
	t.Logf("- engines: %+v\n", r.engines)
	t.Logf("- route: %+v\n", r.m)

	got, err := r.findLeastLoadedEngine(map[string]bool{})
	assert.NoError(t, err)
	want := "engine2"
	assert.Equal(t, want, got)

	got, err = r.findLeastLoadedEngine(map[string]bool{"engine2": true})
	assert.NoError(t, err)
	want = "engine1"
	assert.Equal(t, want, got)
}
