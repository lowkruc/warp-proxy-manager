package store

import (
	"log"
	"sync"
	"time"

	"github.com/lowkruc/warp-proxy-manager/internal/proxy"
)

type MetricsCollector struct {
	store       *Store
	balancer    *proxy.LoadBalancer
	interval    time.Duration
	stopCh      chan struct{}
	mu          sync.RWMutex
	response429 int
	response5xx int
}

func NewMetricsCollector(store *Store, balancer *proxy.LoadBalancer, interval time.Duration) *MetricsCollector {
	return &MetricsCollector{
		store:    store,
		balancer: balancer,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

func (mc *MetricsCollector) Start() {
	go mc.loop()
	log.Printf("[METRICS] Collector started (interval: %s)", mc.interval)
}

func (mc *MetricsCollector) Stop() {
	close(mc.stopCh)
	log.Printf("[METRICS] Collector stopped")
}

func (mc *MetricsCollector) loop() {
	ticker := time.NewTicker(mc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-mc.stopCh:
			return
		case <-ticker.C:
			mc.collect()
		}
	}
}

func (mc *MetricsCollector) collect() {
	stats := mc.balancer.GetHealthy()

	mc.mu.Lock()
	record := &MetricRecord{
		Timestamp:    time.Now(),
		BackendCount: len(stats),
		Response429:  mc.response429,
		Response5xx:  mc.response5xx,
	}
	mc.response429 = 0
	mc.response5xx = 0
	mc.mu.Unlock()

	if err := mc.store.SaveMetric(record); err != nil {
		log.Printf("[METRICS] Failed to save: %v", err)
	}
}

func (mc *MetricsCollector) Track429() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.response429++
}

func (mc *MetricsCollector) Track5xx() {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.response5xx++
}

func (mc *MetricsCollector) GetLatest(window string) (*MetricRecord, error) {
	records, err := mc.store.GetMetrics(window, 1)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return &MetricRecord{}, nil
	}
	return records[0], nil
}

func (mc *MetricsCollector) GetHistory(window string, limit int) ([]*MetricRecord, error) {
	return mc.store.GetMetrics(window, limit)
}
