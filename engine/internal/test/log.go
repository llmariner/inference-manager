package test

import (
	"context"
	"log"
	"strings"
	"testing"

	"github.com/go-logr/stdr"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ContextWithLogger returns a context with a logger that writes to the test log.
func ContextWithLogger(t *testing.T) context.Context {
	logger := log.New(&testLogWriter{t}, "TEST: ", 0)
	ctx := ctrl.LoggerInto(context.Background(), stdr.New(logger))
	stdr.SetVerbosity(8)
	return ctx
}

type testLogWriter struct {
	t *testing.T
}

func (w *testLogWriter) Write(p []byte) (n int, err error) {
	w.t.Log(strings.TrimSpace(string(p)))
	return len(p), nil
}
