package server

import (
	"github.com/go-logr/logr"
)

// New creates a server.
func New(tritonBaseURL string, logger logr.Logger) *S {
	return &S{
		tritonBaseURL: tritonBaseURL,
		logger:        logger.WithName("grpc"),
	}
}

// S is a server.
type S struct {
	tritonBaseURL string

	logger logr.Logger
}
