package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "status":
		cmdStatus()
	case "containers":
		cmdContainers()
	case "scale":
		if len(args) < 1 {
			fmt.Println("Usage: warpctl scale <count>")
			os.Exit(1)
		}
		count, err := strconv.Atoi(args[0])
		if err != nil {
			fmt.Printf("Invalid count: %s\n", args[0])
			os.Exit(1)
		}
		cmdScale(count)
	case "health":
		cmdHealth()
	case "metrics":
		cmdMetrics()
	case "create":
		cmdCreateContainer()
	case "restart":
		if len(args) < 1 {
			fmt.Println("Usage: warpctl restart <id>")
			os.Exit(1)
		}
		cmdRestart(args[0])
	case "delete":
		if len(args) < 1 {
			fmt.Println("Usage: warpctl delete <id>")
			os.Exit(1)
		}
		cmdDelete(args[0])
	case "history":
		cmdHistory()
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Warp Proxy Manager CLI

Usage:
  warpctl status        Show manager status
  warpctl containers    List containers
  warpctl health        Show health status
  warpctl metrics       Show current metrics
  warpctl history       Show scale history
  warpctl create        Create new container
  warpctl scale <n>     Scale to n containers
  warpctl restart <id>  Restart container
  warpctl delete <id>   Delete container

Options:
  -h, --help           Show help
  -v, --version        Show version`)
}

func getBaseURL() string {
	host := os.Getenv("WARP_MANAGER_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("WARP_MANAGER_PORT")
	if port == "" {
		port = "8080"
	}
	return fmt.Sprintf("http://%s:%s", host, port)
}

func apiGet(path string) ([]byte, error) {
	url := getBaseURL() + path
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	token := os.Getenv("WARP_TOKEN")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func apiPost(path string, body interface{}) ([]byte, error) {
	url := getBaseURL() + path
	
	var reqBody io.Reader
	if body != nil {
		jsonBytes, _ := json.Marshal(body)
		reqBody = strings.NewReader(string(jsonBytes))
	}

	req, err := http.NewRequest("POST", url, reqBody)
	if err != nil {
		return nil, err
	}

	token := os.Getenv("WARP_TOKEN")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func apiDelete(path string) error {
	url := getBaseURL() + path
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	token := os.Getenv("WARP_TOKEN")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

func cmdStatus() {
	data, err := apiGet("/api/v1/proxy/stats")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	var stats struct {
		TotalRequests int `json:"total_requests"`
		Total429      int `json:"total_429"`
		ActiveConns   int `json:"active_connections"`
		BackendsHealthy int `json:"backends_healthy"`
	}
	json.Unmarshal(data, &stats)

	fmt.Println("=== Warp Proxy Manager Status ===")
	fmt.Printf("Total Requests:    %d\n", stats.TotalRequests)
	fmt.Printf("Total 429s:        %d\n", stats.Total429)
	fmt.Printf("Active Connections: %d\n", stats.ActiveConns)
	fmt.Printf("Healthy Backends:  %d\n", stats.BackendsHealthy)
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
		ID        string `json:"id"`
		From      int    `json:"from"`
		To        int    `json:"to"`
		Reason    string `json:"reason"`
		Duration  int    `json:"duration_ms"`
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
		Status string `json:"status"`
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
