package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
)

func cmdStatus() {
	data, err := apiGet("/api/v1/proxy/stats")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	var stats struct {
		TotalRequests   int `json:"total_requests"`
		Total429        int `json:"total_429"`
		ActiveConns     int `json:"active_connections"`
		BackendsHealthy int `json:"backends_healthy"`
	}
	json.Unmarshal(data, &stats)

	fmt.Println("=== Warp Proxy Manager Status ===")
	fmt.Printf("Total Requests:     %d\n", stats.TotalRequests)
	fmt.Printf("Total 429s:         %d\n", stats.Total429)
	fmt.Printf("Active Connections: %d\n", stats.ActiveConns)
	fmt.Printf("Healthy Backends:   %d\n", stats.BackendsHealthy)
}

func cmdContainers() {
	data, err := apiGet("/api/v1/containers")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	var result struct {
		Containers []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
			Port   int    `json:"port"`
		} `json:"containers"`
		Total int `json:"total"`
	}
	json.Unmarshal(data, &result)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tSTATUS\tPORT")
	fmt.Fprintln(w, "---\t----\t------\t----")
	for _, c := range result.Containers {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", c.ID, c.Name, c.Status, c.Port)
	}
	w.Flush()
	fmt.Printf("\nTotal: %d\n", result.Total)
}

func cmdScale(count int) {
	fmt.Printf("Scaling to %d containers...\n", count)

	data, err := apiPost(fmt.Sprintf("/api/v1/scaling/scale/%d", count), nil)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	var event struct {
		ID       string `json:"id"`
		From     int    `json:"from"`
		To       int    `json:"to"`
		Reason   string `json:"reason"`
		Duration int64  `json:"duration_ms"`
	}
	json.Unmarshal(data, &event)

	fmt.Printf("Scaled from %d to %d (reason: %s)\n", event.From, event.To, event.Reason)
}

func cmdHealth() {
	data, err := apiGet("/api/v1/health")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	var health struct {
		Status     string `json:"status"`
		Containers struct {
			Total     int `json:"total"`
			Running   int `json:"running"`
			Healthy   int `json:"healthy"`
			Unhealthy int `json:"unhealthy"`
		} `json:"containers"`
	}
	json.Unmarshal(data, &health)

	fmt.Println("=== Health Status ===")
	fmt.Printf("Status: %s\n", health.Status)
	fmt.Printf("Containers: %d total, %d running, %d healthy, %d unhealthy\n",
		health.Containers.Total,
		health.Containers.Running,
		health.Containers.Healthy,
		health.Containers.Unhealthy,
	)
}

func cmdMetrics() {
	data, err := apiGet("/api/v1/metrics?window=current")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	var metrics struct {
		ActiveConns     int `json:"active_connections"`
		TotalRequests   int `json:"total_requests"`
		Total429        int `json:"total_429"`
		BackendsHealthy int `json:"backends"`
	}
	json.Unmarshal(data, &metrics)

	fmt.Println("=== Current Metrics ===")
	fmt.Printf("Active Connections: %d\n", metrics.ActiveConns)
	fmt.Printf("Total Requests:     %d\n", metrics.TotalRequests)
	fmt.Printf("Total 429s:         %d\n", metrics.Total429)
	fmt.Printf("Healthy Backends:   %d\n", metrics.BackendsHealthy)
}

func cmdCreateContainer() {
	fmt.Println("Creating new container...")

	data, err := apiPost("/api/v1/containers", nil)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	var result struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
		Port   int    `json:"port"`
	}
	json.Unmarshal(data, &result)

	fmt.Printf("Created: %s (%s) on port %d\n", result.Name, result.ID, result.Port)
}

func cmdRestart(id string) {
	fmt.Printf("Restarting container %s...\n", id)

	_, err := apiPost(fmt.Sprintf("/api/v1/containers/%s/restart", id), nil)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Container restarted")
}

func cmdDelete(id string) {
	fmt.Printf("Deleting container %s...\n", id)

	if err := apiDelete(fmt.Sprintf("/api/v1/containers/%s", id)); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Container deleted")
}

func cmdHistory() {
	data, err := apiGet("/api/v1/scaling/history?limit=20")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	var result struct {
		Events []struct {
			Timestamp string `json:"timestamp"`
			From      int    `json:"from"`
			To        int    `json:"to"`
			Reason    string `json:"reason"`
		} `json:"events"`
	}
	json.Unmarshal(data, &result)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIMESTAMP\tFROM\tTO\tREASON")
	fmt.Fprintln(w, "---------\t----\t--\t------")
	for _, e := range result.Events {
		fmt.Fprintf(w, "%s\t%d\t%d\t%s\n", e.Timestamp, e.From, e.To, e.Reason)
	}
	w.Flush()
}
