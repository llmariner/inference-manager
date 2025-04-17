package puller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
)

type puller interface {
	Pull(ctx context.Context, modelID string) error
	isDownloaded(modelID string) (bool, error)
}

// NewServer creates a new server instance.
func NewServer(p puller) *Server {
	const queueLengths = 5
	return &Server{
		p:      p,
		pullCh: make(chan string, queueLengths),
		ready:  make(chan struct{}),
	}
}

// Server represents a server that handles pull requests.
type Server struct {
	p      puller
	pullCh chan string

	srv   *http.Server
	ready chan struct{}
}

// Start starts an HTTP server that listens for pull requests.
func (s *Server) Start(port int) error {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	return s.start(l)
}

func (s *Server) start(listener net.Listener) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/pull", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Unsupported method", http.StatusMethodNotAllowed)
			return
		}
		var req pullModelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			log.Printf("Failed to decode the request: %v\n", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.ModelID == "" {
			http.Error(w, "Model ID must be set", http.StatusBadRequest)
			return
		}
		select {
		case s.pullCh <- req.ModelID:
			w.WriteHeader(http.StatusAccepted)
		default:
			w.WriteHeader(http.StatusTooManyRequests)
		}
	})

	mux.HandleFunc("/models/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Unsupported method", http.StatusMethodNotAllowed)
			return
		}

		// Extract the modelID from the path "/models/{modelID}"
		modelID := strings.TrimPrefix(r.URL.Path, "/models/")
		if modelID == "" {
			http.Error(w, "Model ID must be set", http.StatusBadRequest)
			return
		}

		ok, err := s.p.isDownloaded(modelID)
		if err != nil {
			http.Error(w, "Failed to check if the model is downloaded", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "Model not found", http.StatusNotFound)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	s.srv = &http.Server{
		Addr:    listener.Addr().String(),
		Handler: mux,
	}
	close(s.ready)

	log.Printf("Starting HTTP server at %q\n", s.srv.Addr)
	if err := s.srv.Serve(listener); err != http.ErrServerClosed {
		return err
	}

	return nil
}

func (s *Server) shutdown(ctx context.Context) error {
	<-s.ready
	return s.srv.Shutdown(ctx)
}

// ProcessPullRequests processes pull requests.
func (s *Server) ProcessPullRequests(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case modelID := <-s.pullCh:
			if err := s.p.Pull(ctx, modelID); err != nil {
				return fmt.Errorf("pull the model: %s", err)
			}
		}
	}
}

// QueuePullRequest queues a pull request for the given model ID.
func (s *Server) QueuePullRequest(modelID string) {
	s.pullCh <- modelID
}
