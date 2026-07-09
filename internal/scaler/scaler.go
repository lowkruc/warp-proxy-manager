package scaler

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lowkruc/warp-proxy-manager/internal/config"
	"github.com/lowkruc/warp-proxy-manager/internal/docker"
	"github.com/lowkruc/warp-proxy-manager/internal/proxy"
)

type Event struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	From      int       `json:"from"`
	To        int       `json:"to"`
	Reason    string    `json:"reason"`
	Trigger   string    `json:"trigger"`
	Duration  int64     `json:"duration_ms"`
}

type Counter struct {
	mu       sync.RWMutex
	events   []CounterEvent
	Window   time.Duration
	Threshold float64
}

type CounterEvent struct {
	Timestamp time.Time
	BackendID string
	Code      int
}

type Scaler struct {
	mu           sync.RWMutex
	config       *config.Config
	docker       *docker.Client
	balancer     *proxy.LoadBalancer
	counters     map[string]*Counter
	lastScale    time.Time
	events       []*Event
	running      bool
	stopCh       chan struct{}
}

func NewScaler(cfg *config.Config, dockerClient *docker.Client, balancer *proxy.LoadBalancer) *Scaler {
	s := &Scaler{
		config:   cfg,
		docker:   dockerClient,
		balancer: balancer,
		counters: make(map[string]*Counter),
		events:   make([]*Event, 0),
		stopCh:   make(chan struct{}),
	}

	// Initialize counters from triggers
	for _, trigger := range cfg.Scaling.Triggers {
		if trigger.Type == "response_code" {
			s.counters[trigger.Name] = &Counter{
				Window:    trigger.Window,
				Threshold: trigger.Threshold,
			}
		}
	}

	return s
}

func (s *Scaler) Start() {
	s.running = true
	go s.loop()
	log.Printf("[SCALER] Started")
}

func (s *Scaler) Stop() {
	s.running = false
	close(s.stopCh)
	log.Printf("[SCALER] Stopped")
}

func (s *Scaler) loop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.evaluate()
		}
	}
}

func (s *Scaler) TrackResponseCode(backendID string, code int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, trigger := range s.config.Scaling.Triggers {
		if trigger.Type == "response_code" && trigger.ResponseCode == code {
			counter, ok := s.counters[trigger.Name]
			if !ok {
				continue
			}

			counter.mu.Lock()
			counter.events = append(counter.events, CounterEvent{
				Timestamp: time.Now(),
				BackendID: backendID,
				Code:      code,
			})

			// Cleanup old events
			cutoff := time.Now().Add(-counter.Window)
			newEvents := make([]CounterEvent, 0)
			for _, e := range counter.events {
				if e.Timestamp.After(cutoff) {
					newEvents = append(newEvents, e)
				}
			}
			counter.events = newEvents
			counter.mu.Unlock()
		}
	}
}

func (s *Scaler) TrackAllFailed(targetHost string, err error) {
	log.Printf("[SCALER] All backends failed for %s: %v", targetHost, err)
}

func (s *Scaler) evaluate() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check cooldown
	if time.Since(s.lastScale) < s.config.Scaling.Cooldown {
		return
	}

	currentCount := s.docker.RunningCount(context.Background())
	log.Printf("[SCALER] evaluate: running=%d cooldown_ok=%v", currentCount, time.Since(s.lastScale) >= s.config.Scaling.Cooldown)
	if currentCount == 0 {
		return
	}

	// Check scale up triggers
	for _, trigger := range s.config.Scaling.Triggers {
		switch trigger.Type {
		case "response_code":
			if s.shouldScaleResponseCode(trigger, currentCount) {
				return
			}
		case "connection":
			if trigger.ScaleDirection == "up" && s.shouldScaleConnection(trigger, currentCount) {
				return
			}
		}
	}

	// Check scale down triggers
	for _, trigger := range s.config.Scaling.Triggers {
		if trigger.Type == "connection" && trigger.ScaleDirection == "down" {
			if s.shouldScaleDown(trigger, currentCount) {
				return
			}
		}
	}
}

func (s *Scaler) shouldScaleResponseCode(trigger config.ScaleTrigger, currentCount int) bool {
	counter, ok := s.counters[trigger.Name]
	if !ok {
		return false
	}

	counter.mu.RLock()
	defer counter.mu.RUnlock()

	if float64(len(counter.events)) < trigger.Threshold {
		return false
	}

	// Check if all backends are affected
	if trigger.AllBackends {
		affected := make(map[string]bool)
		for _, e := range counter.events {
			affected[e.BackendID] = true
		}

		healthyCount := s.balancer.HealthyCount()
		if len(affected) < healthyCount {
			return false // Not all backends affected
		}
	}

	// Can we scale up?
	if currentCount >= s.config.Scaling.Max {
		log.Printf("[SCALER] Cannot scale up, max reached (%d)", s.config.Scaling.Max)
		return false
	}

	// Do the scaling
	targetCount := currentCount + trigger.ScaleCount
	if targetCount > s.config.Scaling.Max {
		targetCount = s.config.Scaling.Max
	}

	s.scale(targetCount, trigger.Name)
	return true
}

func (s *Scaler) shouldScaleConnection(trigger config.ScaleTrigger, currentCount int) bool {
	stats := s.balancer.GetBackends()
	if len(stats) == 0 {
		return false
	}

	// Calculate average connections
	var totalConns int64
	for _, b := range stats {
		totalConns += b.Connections
	}
	avgConns := float64(totalConns) / float64(len(stats))

	if avgConns <= trigger.Threshold {
		log.Printf("[SCALER] %s: avg=%.1f <= threshold=%.1f, skip", trigger.Name, avgConns, trigger.Threshold)
		return false
	}

	// Can we scale up?
	if currentCount >= s.config.Scaling.Max {
		return false
	}

	targetCount := currentCount + trigger.ScaleCount
	if targetCount > s.config.Scaling.Max {
		targetCount = s.config.Scaling.Max
	}

	s.scale(targetCount, trigger.Name)
	return true
}

func (s *Scaler) shouldScaleDown(trigger config.ScaleTrigger, currentCount int) bool {
	if currentCount <= s.config.Scaling.Min {
		return false
	}

	stats := s.balancer.GetBackends()
	if len(stats) == 0 {
		return false
	}

	var totalConns int64
	for _, b := range stats {
		totalConns += b.Connections
	}
	avgConns := float64(totalConns) / float64(len(stats))

	if avgConns >= trigger.Threshold {
		return false
	}

	targetCount := currentCount - trigger.ScaleCount
	if targetCount < s.config.Scaling.Min {
		targetCount = s.config.Scaling.Min
	}

	s.scale(targetCount, trigger.Name)
	return true
}

func (s *Scaler) scale(targetCount int, reason string) {
	currentCount := s.docker.RunningCount(context.Background())

	start := time.Now()

	event := &Event{
		ID:        uuid.New().String(),
		Timestamp: time.Now(),
		From:      currentCount,
		To:        targetCount,
		Reason:    reason,
	}

	if targetCount > currentCount {
		// Scale up (sequential to avoid port conflicts)
		log.Printf("[SCALER] Scaling UP: %d -> %d (reason: %s)", currentCount, targetCount, reason)
		for i := 0; i < targetCount-currentCount; i++ {
			container, err := s.docker.CreateContainer(context.Background(), "")
			if err != nil {
				log.Printf("[SCALER] Failed to create container: %v", err)
				continue
			}
			log.Printf("[SCALER] Created container: %s", container.Name)
		}
	} else if targetCount < currentCount {
		// Scale down
		log.Printf("[SCALER] Scaling DOWN: %d -> %d (reason: %s)", currentCount, targetCount, reason)
		containers, _ := s.docker.ListContainers(context.Background())
		removed := 0
		for _, c := range containers {
			if removed >= currentCount-targetCount {
				break
			}
			if err := s.docker.RemoveContainer(context.Background(), c.ID, true); err != nil {
				log.Printf("[SCALER] Failed to remove container %s: %v", c.ID, err)
				continue
			}
			log.Printf("[SCALER] Removed container: %s", c.Name)
			removed++
		}
	}

	event.Duration = time.Since(start).Milliseconds()
	s.events = append(s.events, event)
	s.lastScale = time.Now()

	// Keep only last 100 events
	if len(s.events) > 100 {
		s.events = s.events[len(s.events)-100:]
	}
}

func (s *Scaler) ManualScale(count int) *Event {
	s.mu.Lock()
	defer s.mu.Unlock()

	if count == 0 {
		count = s.config.Scaling.Min
	}

	if count > s.config.Scaling.Max {
		count = s.config.Scaling.Max
	}

	s.scale(count, "manual")
	return s.events[len(s.events)-1]
}

func (s *Scaler) GetHistory(limit int) []*Event {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if limit <= 0 || limit > len(s.events) {
		limit = len(s.events)
	}

	result := make([]*Event, limit)
	copy(result, s.events[len(s.events)-limit:])
	return result
}

func (s *Scaler) GetCounterValue(triggerName string) int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	counter, ok := s.counters[triggerName]
	if !ok {
		return 0
	}

	counter.mu.RLock()
	defer counter.mu.RUnlock()

	return len(counter.events)
}
