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

func (s *ProcessingStats) RuntimeLatencyMs() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runtimeLatencyMs
}

func (s *ProcessingStats) RuntimeTimeToFirstTokenMs() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runtimeTimeToFirstTokenMs
}
