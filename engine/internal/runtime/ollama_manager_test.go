package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	testutil "github.com/llmariner/inference-manager/common/pkg/test"
	"github.com/stretchr/testify/assert"
)

func TestOllamaPullModel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			t.Errorf("expected POST request; got %s", r.Method)
			return
		}
		switch r.URL.Path {
		case "/pull":
			w.WriteHeader(http.StatusAccepted)
			return
		case "/api/show":
			w.WriteHeader(http.StatusOK)
			return
		default:
			w.WriteHeader(http.StatusNotFound)
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
	}))
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	const (
		stsName = "test"
		modelID = "mid-0"
	)
	var tests = []struct {
		name   string
		rt     *runtime
		models *ollamaModel
	}{
		{
			name: "runtime is not ready",
			rt:   newPendingRuntime(stsName),
		},
		{
			name: "new model",
			rt:   newReadyRuntime(stsName, addr, 1),
		},
		{
			name:   "already pulled",
			rt:     newReadyRuntime(stsName, addr, 1),
			models: &ollamaModel{id: modelID},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rtClient := &fakeClient{deployed: map[string]bool{}}
			mgr := &OllamaManager{
				ollamaClient: rtClient,
				pullerAddr:   addr,
				runtime:      test.rt,
				models:       make(map[string]*ollamaModel),
			}

			ctx, cancel := context.WithTimeout(testutil.ContextWithLogger(t), 2*time.Second)
			defer cancel()

			if !test.rt.ready {
				go func() {
					// emulate runtime to be ready
					select {
					case <-ctx.Done():
						return
					case <-time.After(300 * time.Millisecond):
						mgr.mu.Lock()
						mgr.runtime.becomeReady(addr, 1, 1, testutil.NewTestLogger(t))
						mgr.runtime.closeWaitChs("")
						mgr.mu.Unlock()
					}
				}()
			}
			if test.models != nil {
				mgr.models[modelID] = test.models

				go func() {
					// emulate model to be ready
					select {
					case <-ctx.Done():
						return
					case <-time.After(300 * time.Millisecond):
						mgr.mu.Lock()
						for _, ch := range mgr.models[modelID].waitChs {
							close(ch)
						}
						mgr.models[modelID] = &ollamaModel{id: modelID, ready: true}
						mgr.mu.Unlock()
					}
				}()
			}

			err := mgr.PullModel(ctx, modelID)
			assert.NoError(t, err)
		})
	}
}
