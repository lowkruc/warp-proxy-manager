package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNew(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer s.Close()

	if s == nil {
		t.Error("New() returned nil")
	}

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("Database file not created")
	}
}

func TestStore_SaveAndGetMetrics(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(filepath.Join(dir, "test.db"))
	defer s.Close()

	m := &MetricRecord{
		ActiveConns:  10,
		BackendCount: 3,
		Response429:  1,
	}
	if err := s.SaveMetric(m); err != nil {
		t.Fatalf("SaveMetric() error = %v", err)
	}

	metrics, err := s.GetMetrics("1h", 10)
	if err != nil {
		t.Fatalf("GetMetrics() error = %v", err)
	}
	if len(metrics) != 1 {
		t.Errorf("GetMetrics() count = %d, want 1", len(metrics))
	}
	if metrics[0].ActiveConns != 10 {
		t.Errorf("ActiveConns = %d, want 10", metrics[0].ActiveConns)
	}
}

func TestStore_SaveAndGetScaleEvents(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(filepath.Join(dir, "test.db"))
	defer s.Close()

	e := &ScaleEventRecord{
		Trigger: "high_load",
		From:    2,
		To:      3,
		Reason:  "connections exceeded threshold",
	}
	if err := s.SaveScaleEvent(e); err != nil {
		t.Fatalf("SaveScaleEvent() error = %v", err)
	}

	events, err := s.GetScaleEvents(10)
	if err != nil {
		t.Fatalf("GetScaleEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Errorf("GetScaleEvents() count = %d, want 1", len(events))
	}
	if events[0].Trigger != "high_load" {
		t.Errorf("Trigger = %q, want high_load", events[0].Trigger)
	}
}

func TestStore_Cleanup(t *testing.T) {
	dir := t.TempDir()
	s, _ := New(filepath.Join(dir, "test.db"))
	defer s.Close()

	for i := 0; i < 5; i++ {
		s.SaveMetric(&MetricRecord{ActiveConns: i})
	}

	if err := s.Cleanup(0); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}

	metrics, _ := s.GetMetrics("1h", 100)
	if len(metrics) != 0 {
		t.Errorf("After Cleanup() count = %d, want 0", len(metrics))
	}
}
