package health

import (
	"fmt"
	"log"
	"net/http"
	"strings"
)

// NewProbeHandler returns a new ProbeHandler.
func NewProbeHandler() *ProbeHandler {
	return &ProbeHandler{}
}

type probe interface {
	IsReady() (bool, string)
}

// ProbeHandler aggregates health probers.
type ProbeHandler struct {
	probes []probe
}

// AddProbe adds a health prober.
func (h *ProbeHandler) AddProbe(p probe) {
	h.probes = append(h.probes, p)
}

// ProbeHandler writes a result of the health probe.
func (h *ProbeHandler) ProbeHandler(resp http.ResponseWriter, _ *http.Request) {
	var msgs []string

	for _, p := range h.probes {
		if r, msg := p.IsReady(); !r {
			msgs = append(msgs, msg)
		}
	}

	if len(msgs) > 0 {
		http.Error(resp, strings.Join(msgs, ","), http.StatusServiceUnavailable)
		return
	}

	if _, err := fmt.Fprint(resp, "ok"); err != nil {
		log.Printf("Failed to write health response: %s", err)
	}
}
