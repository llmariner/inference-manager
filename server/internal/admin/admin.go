package admin

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/llm-operator/inference-manager/server/internal/infprocessor"
)

// NewHandler returns a new admin handler.
func NewHandler(
	infProcessor *infprocessor.P,
) *Handler {
	return &Handler{
		infProcessor: infProcessor,
	}
}

// Handler handles admin requests.
type Handler struct {
	infProcessor *infprocessor.P
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
		log.Printf("Failed to write response: %s\n", err)
	}
}
