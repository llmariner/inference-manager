package puller

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestServer(t *testing.T) {
	l, err := net.Listen("tcp", ":0")
	assert.NoError(t, err)

	puller := &fakePuller{
		pulledModelIDs: make(map[string]bool),
	}
	srv := NewServer(puller)
	go func() {
		err := srv.start(l)
		if err != nil {
			assert.True(t, errors.Is(err, context.Canceled))
			return
		}
		assert.NoError(t, err)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		err := srv.ProcessPullRequests(ctx)
		if err != nil {
			assert.True(t, errors.Is(err, context.Canceled))
			return
		}
		assert.NoError(t, err)
	}()

	<-srv.ready

	client := NewClient(fmt.Sprintf("localhost:%d", l.Addr().(*net.TCPAddr).Port))
	status, err := client.GetModel(ctx, "m0")
	assert.NoError(t, err)
	assert.Equal(t, status, http.StatusNotFound)

	// Empty model ID. Failed to pull.
	err = client.PullModel(ctx, "")
	assert.Error(t, err)

	err = client.PullModel(ctx, "m0")
	assert.NoError(t, err)

	assert.Eventually(t, func() bool {
		ok, err := puller.isDownloaded("m0")
		assert.NoError(t, err)
		return ok
	}, 5*time.Second, 10*time.Millisecond, "Model m0 should be pulled")

	status, err = client.GetModel(ctx, "m0")
	assert.NoError(t, err)
	assert.Equal(t, status, http.StatusOK)

	err = srv.shutdown(ctx)
	assert.NoError(t, err)
}

type fakePuller struct {
	pulledModelIDs map[string]bool
	mu             sync.Mutex
}

func (f *fakePuller) Pull(ctx context.Context, modelID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pulledModelIDs[modelID] = true
	return nil
}

func (f *fakePuller) isDownloaded(modelID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pulledModelIDs[modelID], nil
}
