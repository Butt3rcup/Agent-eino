package handler

import (
	"sort"
	"sync"

	"go-eino-agent/internal/rag"
)

type ComponentState struct {
	Status   string `json:"status"`
	Optional bool   `json:"optional,omitempty"`
	LastError string `json:"last_error,omitempty"`
}

type ModeState struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type RuntimeSnapshot struct {
	Ready              bool                      `json:"ready"`
	Status             string                    `json:"status"`
	DegradedComponents []string                  `json:"degraded_components,omitempty"`
	Components         map[string]ComponentState `json:"components,omitempty"`
	Modes              map[string]ModeState      `json:"modes,omitempty"`
	Background         *rag.PersistQueueStats    `json:"background,omitempty"`
}

type RuntimeStatus struct {
	mu         sync.RWMutex
	components map[string]ComponentState
	modes      map[string]ModeState
}

func NewRuntimeStatus() *RuntimeStatus {
	return &RuntimeStatus{
		components: make(map[string]ComponentState),
		modes:      make(map[string]ModeState),
	}
}

func (s *RuntimeStatus) SetComponent(name string, optional bool, err error) {
	state := ComponentState{Status: "up", Optional: optional}
	if err != nil {
		state.Status = "down"
		state.LastError = err.Error()
	}
	s.mu.Lock()
	s.components[name] = state
	s.mu.Unlock()
}

func (s *RuntimeStatus) SetMode(name string, available bool, reason string) {
	s.mu.Lock()
	s.modes[name] = ModeState{Available: available, Reason: reason}
	s.mu.Unlock()
}

func (s *RuntimeStatus) Mode(name string) (ModeState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mode, ok := s.modes[name]
	return mode, ok
}

func (s *RuntimeStatus) Snapshot(background *rag.PersistQueueStats) RuntimeSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	components := make(map[string]ComponentState, len(s.components))
	degraded := make([]string, 0)
	ready := true
	status := "ok"
	for name, state := range s.components {
		components[name] = state
		if state.Status == "up" {
			continue
		}
		if state.Optional {
			degraded = append(degraded, name)
			status = "degraded"
			continue
		}
		ready = false
		status = "down"
	}
	sort.Strings(degraded)

	modes := make(map[string]ModeState, len(s.modes))
	for name, mode := range s.modes {
		modes[name] = mode
	}

	return RuntimeSnapshot{
		Ready:              ready,
		Status:             status,
		DegradedComponents: degraded,
		Components:         components,
		Modes:              modes,
		Background:         background,
	}
}

