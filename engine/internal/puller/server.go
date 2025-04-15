package puller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/llmariner/inference-manager/engine/internal/modeldownloader"
)

// NewServer creates a new server instance.
func NewServer(p *P) *Server {
	const queueLengths = 5
	return &Server{
		p:      p,
		pullCh: make(chan string, queueLengths),
	}
}

// Server represents a server that handles pull requests.
type Server struct {
	p      *P
	pullCh chan string
}

// Start starts an HTTP server that listens for pull requests.
func (s *Server) Start(ctx context.Context, port int) error {
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

		cpath := modeldownloader.CompletionIndicationFilePath(ModelDir(), modelID)
		// Check if the file exists
		if _, err := os.Stat(cpath); os.IsNotExist(err) {
			http.Error(w, "Model not found", http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	log.Printf("Starting HTTP server at %q\n", srv.Addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}

	return nil
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
