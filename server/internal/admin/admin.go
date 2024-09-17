package admin

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/llm-operator/inference-manager/server/internal/infprocessor"
)

// NewHandler returns a new admin handler.
func NewHandler(
	infProcessor *infprocessor.P,
	logger logr.Logger,
) *Handler {
	return &Handler{
		infProcessor: infProcessor,
		logger:       logger.WithName("admin"),
	}
}

// Handler handles admin requests.
type Handler struct {
	infProcessor *infprocessor.P
	logger       logr.Logger
}

// AdminHandler handles admin requests.
func (h *Handler) AdminHandler(resp http.ResponseWriter, _ *http.Request) {
	s := h.infProcessor.DumpStatus()
	b, err := json.Marshal(s)
	if err != nil {
		http.Error(resp, err.Error(), http.StatusInternalServerError)
		return
	}

	if _, err := resp.Write(b); err != nil {
		h.logger.Error(err, "Failed to write response")
	}
}

// Run runs the admin server.
func (h *Handler) Run(port int) error {
	h.logger.Info("Starting admin server...", "port", port)

	adminMux := http.NewServeMux()
	adminMux.HandleFunc("/admin", h.AdminHandler)
	srv := http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: adminMux,
	}

	if err := srv.ListenAndServe(); err != nil {
		return err
	}
	h.logger.Info("Stopped admin server")
	return nil
}
