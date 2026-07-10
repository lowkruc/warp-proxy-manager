package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"
)

const (
	Version = "0.1.0"
	InstallDir = "/opt/warp-proxy-manager"
)

var reader = bufio.NewReader(os.Stdin)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "init":
		cmdInit()
	case "start":
		cmdStart()
	case "stop":
		cmdStop()
	case "uninstall":
		cmdUninstall()
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
	case "version", "-v", "--version":
		fmt.Printf("warpctl version %s\n", Version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`
 ╦╔╗╔╔╦╗╔═╗╦═╗╔╦╗╔═╗╔═╗╔═╗ ╔═╗╔═╗╔═╗╔═╗╔╗ ╔═╗╔╦╗
 ║║║║ ║ ║╣ ╠╦╝║║║╠═╣║  ╚═╗ ╠═╣║  ║ ║║╣ ╠╩╗║ ║ ║ ║
 ╩╝╚╝ ╩ ╚═╝╩╚═╩ ╩╩ ╩╚═╝╚═╝ ╩ ╩╚═╝╚═╝╚═╝╚═╝╚═╝ ╩ 

Usage:
  warpctl init             Interactive setup
  warpctl start            Start the manager
  warpctl stop             Stop the manager
  warpctl uninstall        Remove everything

  warpctl status           Show manager status
  warpctl containers       List containers
  warpctl health           Show health status
  warpctl metrics          Show current metrics
  warpctl history          Show scale history
  warpctl create           Create new container
  warpctl scale <n>        Scale to n containers
  warpctl restart <id>     Restart container
  warpctl delete <id>      Delete container

Options:
  -h, --help              Show help
  -v, --version           Show version`)
}

// ==================== INIT ====================

type Config struct {
	Manager struct {
		APIPort  int    `yaml:"api_port"`
		LogLevel string `yaml:"log_level"`
	} `yaml:"manager"`
	Proxy struct {
		Listen string `yaml:"listen"`
		Auth   struct {
			Enabled bool     `yaml:"enabled"`
			Users   []struct {
				User string `yaml:"user"`
				Pass string `yaml:"pass"`
			} `yaml:"users"`
		} `yaml:"auth"`
		Timeout struct {
			Connect string `yaml:"connect"`
			Idle    string `yaml:"idle"`
		} `yaml:"timeout"`
	} `yaml:"proxy"`
	Scaling struct {
		Min      int    `yaml:"min"`
		Max      int    `yaml:"max"`
		Cooldown string `yaml:"cooldown"`
	} `yaml:"scaling"`
	LoadBalancer struct {
		Algorithm   string `yaml:"algorithm"`
		HealthCheck struct {
			Enabled            bool `yaml:"enabled"`
			Interval           string `yaml:"interval"`
			Timeout            string `yaml:"timeout"`
			UnhealthyThreshold int  `yaml:"unhealthy_threshold"`
		} `yaml:"health_check"`
	} `yaml:"loadbalancer"`
	Docker struct {
		Image       string            `yaml:"image"`
		Network     string            `yaml:"network"`
		Prefix      string            `yaml:"prefix"`
		MemoryLimit string            `yaml:"memory_limit"`
		CPULimit    string            `yaml:"cpu_limit"`
		Env         map[string]string `yaml:"env"`
	} `yaml:"docker"`
}

func prompt(question, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", question, defaultVal)
	} else {
		fmt.Printf("%s: ", question)
	}
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}

func promptYesNo(question string, defaultYes bool) bool {
	defaultVal := "y"
	if !defaultYes {
		defaultVal = "n"
	}
	fmt.Printf("%s (y/n) [%s]: ", question, defaultVal)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	if input == "" {
		return defaultYes
	}
	return input == "y" || input == "yes"
}

func cmdInit() {
	fmt.Println("=== Warp Proxy Manager Setup ===")
	fmt.Println()

	// Check if already initialized
	if _, err := os.Stat(InstallDir); err == nil {
		fmt.Printf("Already initialized at %s\n", InstallDir)
		if !promptYesNo("Reconfigure?", false) {
			return
		}
	}

	// Docker socket mode
	fmt.Println("\n--- Docker Socket ---")
	fmt.Println("1) Unix socket (local)")
	fmt.Println("2) TCP with TLS (remote)")
	socketMode := prompt("Docker socket mode", "1")

	dockerSocket := "/var/run/docker.sock"
	dockerHost := ""
	dockerTLSCert := ""

	if socketMode == "2" {
		dockerHost = prompt("Docker host", "tcp://remote:2376")
		dockerTLSCert = prompt("TLS cert path", "/etc/docker/certs/client")
	}

	// Ports
	fmt.Println("\n--- Ports ---")
	apiPort := prompt("API port", "8080")
	socksPort := prompt("SOCKS5 port", "1080")

	// Scaling
	fmt.Println("\n--- Scaling ---")
	minContainers := prompt("Min containers", "3")
	maxContainers := prompt("Max containers", "10")
	cooldown := prompt("Cooldown", "60s")

	// Load balancer
	fmt.Println("\n--- Load Balancer ---")
	fmt.Println("1) roundrobin")
	fmt.Println("2) leastconn")
	fmt.Println("3) iphash")
	algoChoice := prompt("Algorithm", "1")
	algorithm := "roundrobin"
	switch algoChoice {
	case "2":
		algorithm = "leastconn"
	case "3":
		algorithm = "iphash"
	}

	// Auth
	fmt.Println("\n--- Authentication ---")
	authEnabled := promptYesNo("Enable SOCKS5 authentication?", false)
	var authUsers []string
	if authEnabled {
		user := prompt("Username", "admin")
		pass := prompt("Password (will be stored in plain text)", "")
		if user != "" && pass != "" {
			authUsers = append(authUsers, fmt.Sprintf(`      - user: %s
        pass: "%s"`, user, pass))
		}
	}

	// Resources
	fmt.Println("\n--- Container Resources ---")
	memoryLimit := prompt("Memory limit per container", "150m")
	cpuLimit := prompt("CPU limit per container", "0.15")
	warpRotation := prompt("WARP rotation interval (seconds)", "60")

	// Generate config.yaml
	configContent := fmt.Sprintf(`manager:
  api_port: %s
  log_level: info

proxy:
  listen: ":%s"
  auth:
    enabled: %v
    users:
%s
  timeout:
    connect: 5s
    idle: 30s

scaling:
  min: %s
  max: %s
  cooldown: %s
  triggers: []

loadbalancer:
  algorithm: %s
  health_check:
    enabled: true
    interval: 10s
    timeout: 5s
    unhealthy_threshold: 3

docker:
  image: "ghcr.io/lowkruc/warp-proxy:latest"
  network: "warp-net"
  prefix: "warp-proxy"
  memory_limit: "%s"
  cpu_limit: "%s"
  env:
    WARP_SLEEP: "5"
    WARP_ROTATION_INTERVAL: "%s"
`, apiPort, socksPort, authEnabled, strings.Join(authUsers, "\n"),
		minContainers, maxContainers, cooldown,
		algorithm, memoryLimit, cpuLimit, warpRotation)

	// Generate docker-compose.yml
	volumeMode := ""
	if dockerSocket == "" {
		// TCP mode
		volumeMode = fmt.Sprintf(`      - %s:/certs/client:ro`, dockerTLSCert)
	}

	var dockerCompose string
	if dockerSocket != "" {
		dockerCompose = fmt.Sprintf(`services:
  manager:
    image: ghcr.io/lowkruc/warp-proxy-manager:latest
    container_name: warp-manager
    ports:
      - "%s:%s"
      - "%s:%s"
    volumes:
      - %s:/var/run/docker.sock
      - ./config.yaml:/app/config.yaml
      - ./data:/app/data
    networks:
      - warp-net
    restart: unless-stopped

networks:
  warp-net:
    name: warp-net
    driver: bridge
`, apiPort, apiPort, socksPort, socksPort, dockerSocket)
	} else {
		dockerCompose = fmt.Sprintf(`services:
  manager:
    image: ghcr.io/lowkruc/warp-proxy-manager:latest
    container_name: warp-manager
    ports:
      - "%s:%s"
      - "%s:%s"
    environment:
      - DOCKER_HOST=%s
      - DOCKER_TLS_VERIFY=1
      - DOCKER_CERT_PATH=/certs/client
    volumes:
      - %s
      - ./config.yaml:/app/config.yaml
      - ./data:/app/data
    networks:
      - warp-net
    restart: unless-stopped

networks:
  warp-net:
    name: warp-net
    driver: bridge
`, apiPort, apiPort, socksPort, socksPort, dockerHost, volumeMode)
	}

	// Create directories
	fmt.Println()
	if err := os.MkdirAll(InstallDir, 0755); err != nil {
		fmt.Printf("Error creating directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Join(InstallDir, "data"), 0755); err != nil {
		fmt.Printf("Error creating data directory: %v\n", err)
		os.Exit(1)
	}

	// Write config.yaml
	if err := os.WriteFile(filepath.Join(InstallDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		fmt.Printf("Error writing config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Config written to %s/config.yaml\n", InstallDir)

	// Write docker-compose.yml
	if err := os.WriteFile(filepath.Join(InstallDir, "docker-compose.yml"), []byte(dockerCompose), 0644); err != nil {
		fmt.Printf("Error writing docker-compose: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Docker Compose written to %s/docker-compose.yml\n", InstallDir)

	fmt.Println()
	fmt.Println("Setup complete!")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  warpctl start      Start the manager")
	fmt.Println("  warpctl stop       Stop the manager")
	fmt.Println("  warpctl status     Check status")
	fmt.Println("  warpctl help       Show all commands")
}

// ==================== START / STOP ====================

func cmdStart() {
	ensureInitialized()

	fmt.Println("Starting Warp Proxy Manager...")
	cmd := exec.Command("docker", "compose", "-f", filepath.Join(InstallDir, "docker-compose.yml"), "up", "-d")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Error starting: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Started")
	fmt.Println()
	fmt.Println("API: http://localhost:8080")
	fmt.Println("SOCKS5: localhost:1080")
}

func cmdStop() {
	ensureInitialized()

	fmt.Println("Stopping Warp Proxy Manager...")
	cmd := exec.Command("docker", "compose", "-f", filepath.Join(InstallDir, "docker-compose.yml"), "down")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Error stopping: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Stopped")
}

// ==================== UNINSTALL ====================

func cmdUninstall() {
	ensureInitialized()

	fmt.Println("=== Warp Proxy Manager Uninstall ===")
	fmt.Println()

	// Stop containers
	fmt.Println("Stopping containers...")
	cmd := exec.Command("docker", "compose", "-f", filepath.Join(InstallDir, "docker-compose.yml"), "down", "-v")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()

	// Remove warp-proxy containers
	fmt.Println("Removing warp-proxy containers...")
	listCmd := exec.Command("docker", "ps", "-a", "--filter", "label=warp-proxy-managed=true", "--format", "{{.ID}}")
	output, _ := listCmd.Output()
	if ids := strings.TrimSpace(string(output)); ids != "" {
		for _, id := range strings.Split(ids, "\n") {
			rmCmd := exec.Command("docker", "rm", "-f", id)
			_ = rmCmd.Run()
		}
	}

	// Remove install directory
	fmt.Printf("Removing %s...\n", InstallDir)
	os.RemoveAll(InstallDir)

	// Remove binary
	binaryPath, _ := exec.LookPath("warpctl")
	if binaryPath != "" {
		fmt.Println("Removing warpctl binary...")
		os.Remove(binaryPath)
	}

	fmt.Println()
	fmt.Println("✓ Uninstalled")
	fmt.Println("  Docker images not removed (run 'docker image prune' to clean)")
}

// ==================== API COMMANDS ====================

func ensureInitialized() {
	if _, err := os.Stat(filepath.Join(InstallDir, "docker-compose.yml")); os.IsNotExist(err) {
		fmt.Println("Not initialized. Run 'warpctl init' first.")
		os.Exit(1)
	}
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

func init() {
	// Suppress log output for clean UI
	_ = runtime.GOOS
}
