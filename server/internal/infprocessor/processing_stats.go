package infprocessor

import "sync"

// ProcessingStats holds stats about processing.
type ProcessingStats struct {
	runtimeLatencyMs          int32
	runtimeTimeToFirstTokenMs int32

	mu sync.Mutex
}

func (s *ProcessingStats) setRuntimeLatencyMs(v int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtimeLatencyMs = v
}

func (s *ProcessingStats) setRuntimeTimeToFirstTokenMsIfUnset(v int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtimeTimeToFirstTokenMs == 0 {
		s.runtimeTimeToFirstTokenMs = v
	}
}

// RuntimeLatencyMs returns the runtime latency in milliseconds.
func (s *ProcessingStats) RuntimeLatencyMs() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runtimeLatencyMs
}

// RuntimeTimeToFirstTokenMs returns the time to first token in milliseconds.
func (s *ProcessingStats) RuntimeTimeToFirstTokenMs() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runtimeTimeToFirstTokenMs
}
