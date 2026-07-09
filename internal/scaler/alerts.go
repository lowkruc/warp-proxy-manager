package scaler

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"time"
)

type AlertConfig struct {
	Enabled   bool     `yaml:"enabled"`
	Webhook   string   `yaml:"webhook"`
	Events    []string `yaml:"events"` // scale_up, scale_down, unhealthy
}

type Alert struct {
	ID        string      `json:"id"`
	Type      string      `json:"type"` // scale_up, scale_down, unhealthy
	Timestamp time.Time   `json:"timestamp"`
	Data      interface{} `json:"data"`
}

type AlertManager struct {
	config  AlertConfig
	client  *http.Client
	stopCh  chan struct{}
}

func NewAlertManager(cfg AlertConfig) *AlertManager {
	return &AlertManager{
		config: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		stopCh: make(chan struct{}),
	}
}

func (am *AlertManager) SendScaleUp(from, to int, reason string) {
	if !am.config.Enabled {
		return
	}

	alert := Alert{
		Type:      "scale_up",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"from":   from,
			"to":     to,
			"reason": reason,
		},
	}

	am.send(alert)
}

func (am *AlertManager) SendScaleDown(from, to int, reason string) {
	if !am.config.Enabled {
		return
	}

	alert := Alert{
		Type:      "scale_down",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"from":   from,
			"to":     to,
			"reason": reason,
		},
	}

	am.send(alert)
}

func (am *AlertManager) SendUnhealthy(containerID, containerName string) {
	if !am.config.Enabled {
		return
	}

	alert := Alert{
		Type:      "unhealthy",
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"container_id":   containerID,
			"container_name": containerName,
		},
	}

	am.send(alert)
}

func (am *AlertManager) send(alert Alert) {
	// Check if event type is enabled
	if !am.isEventEnabled(alert.Type) {
		return
	}

	data, err := json.Marshal(alert)
	if err != nil {
		log.Printf("[ALERT] Failed to marshal: %v", err)
		return
	}

	go func() {
		resp, err := am.client.Post(
			am.config.Webhook,
			"application/json",
			bytes.NewReader(data),
		)
		if err != nil {
			log.Printf("[ALERT] Failed to send: %v", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			log.Printf("[ALERT] Webhook returned %d", resp.StatusCode)
			return
		}

		log.Printf("[ALERT] Sent %s alert", alert.Type)
	}()
}

func (am *AlertManager) isEventEnabled(eventType string) bool {
	if len(am.config.Events) == 0 {
		return true // all events enabled
	}
	for _, e := range am.config.Events {
		if e == eventType || e == "*" {
			return true
		}
	}
	return false
}

func (am *AlertManager) Stop() {
	close(am.stopCh)
}
