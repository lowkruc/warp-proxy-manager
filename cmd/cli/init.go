package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

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
		Triggers []ScaleTrigger `yaml:"triggers"`
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

type ScaleTrigger struct {
	Name           string  `yaml:"name"`
	Type           string  `yaml:"type"`
	ResponseCode   int     `yaml:"response_code,omitempty"`
	Threshold      float64 `yaml:"threshold"`
	Window         string  `yaml:"window,omitempty"`
	ScaleDirection string  `yaml:"scale_direction"`
	ScaleCount     int     `yaml:"scale_count"`
	Cooldown       string  `yaml:"cooldown"`
	AllBackends    bool    `yaml:"all_backends,omitempty"`
}

func cmdInit() {
	fmt.Println("═══════════════════════════════════════")
	fmt.Println("   Warp Proxy Manager Setup")
	fmt.Println("═══════════════════════════════════════")
	fmt.Println()

	// Check if already initialized
	if _, err := os.Stat(InstallDir); err == nil {
		fmt.Printf("Already initialized at %s\n", InstallDir)
		if !promptYesNo("Reconfigure?", false) {
			return
		}
	}

	// Docker socket mode
	dockerSocket, dockerHost, dockerTLSCert := promptDockerConfig()

	// Ports
	apiPort, socksPort := promptPortConfig()

	// Scaling
	minContainers, maxContainers, cooldown := promptScalingConfig()

	// Triggers
	triggers := promptTriggerConfig(minContainers, maxContainers)

	// Load balancer
	algorithm := promptLoadBalancerConfig()

	// Auth
	authEnabled, authUsers := promptAuthConfig()

	// Resources
	memoryLimit, cpuLimit, warpRotation := promptResourceConfig()

	// Generate config.yaml
	configContent := generateConfig(
		apiPort, socksPort,
		minContainers, maxContainers, cooldown,
		triggers,
		algorithm,
		authEnabled, authUsers,
		memoryLimit, cpuLimit, warpRotation,
	)

	// Generate docker-compose.yml
	dockerCompose := generateDockerCompose(
		apiPort, socksPort,
		dockerSocket, dockerHost, dockerTLSCert,
	)

	// Write files
	fmt.Println()
	if err := os.MkdirAll(InstallDir, 0755); err != nil {
		fmt.Printf("Error creating directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Join(InstallDir, "data"), 0755); err != nil {
		fmt.Printf("Error creating data directory: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(filepath.Join(InstallDir, "config.yaml"), []byte(configContent), 0644); err != nil {
		fmt.Printf("Error writing config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Config written to %s/config.yaml\n", InstallDir)

	if err := os.WriteFile(filepath.Join(InstallDir, "docker-compose.yml"), []byte(dockerCompose), 0644); err != nil {
		fmt.Printf("Error writing docker-compose: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Docker Compose written to %s/docker-compose.yml\n", InstallDir)

	fmt.Println()
	fmt.Println("═══════════════════════════════════════")
	fmt.Println("   Setup complete!")
	fmt.Println("═══════════════════════════════════════")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  warpctl start      Start the manager")
	fmt.Println("  warpctl stop       Stop the manager")
	fmt.Println("  warpctl status     Check status")
}

func promptDockerConfig() (socket, host, cert string) {
	fmt.Println("\n--- Docker Socket ---")
	fmt.Println("1) Unix socket (local)")
	fmt.Println("2) TCP with TLS (remote)")
	choice := prompt("Select", "1")

	if choice == "2" {
		host = prompt("Docker host", "tcp://remote:2376")
		cert = prompt("TLS cert path", "/etc/docker/certs/client")
		return "", host, cert
	}
	return "/var/run/docker.sock", "", ""
}

func promptPortConfig() (apiPort, socksPort string) {
	fmt.Println("\n--- Ports ---")
	apiPort = prompt("API port", "8080")
	socksPort = prompt("SOCKS5 port", "1080")
	return
}

func promptScalingConfig() (min, max, cooldown string) {
	fmt.Println("\n--- Scaling ---")
	min = prompt("Min containers", "3")
	max = prompt("Max containers", "10")
	cooldown = prompt("Cooldown", "60s")
	return
}

func promptTriggerConfig(minContainers, maxContainers string) []ScaleTrigger {
	fmt.Println("\n--- Scaling Triggers ---")
	addTriggers := promptYesNo("Add scaling triggers?", true)
	if !addTriggers {
		return nil
	}

	var triggers []ScaleTrigger

	for {
		fmt.Printf("\n--- New Trigger (%d configured) ---\n", len(triggers)+1)
		fmt.Println("1) Connection count (scale up/down)")
		fmt.Println("2) Response code 429 (rate limit)")
		fmt.Println("3) Response code 5xx (server error)")
		typeChoice := prompt("Trigger type", "1")

		var trigger ScaleTrigger

		switch typeChoice {
		case "2":
			trigger = trigger429()
		case "3":
			trigger = trigger5xx()
		default:
			trigger = triggerConnection(minContainers, maxContainers)
		}

		triggers = append(triggers, trigger)

		if !promptYesNo("Add another trigger?", false) {
			break
		}
	}

	return triggers
}

func triggerConnection(minContainers, maxContainers string) ScaleTrigger {
	fmt.Println("\nDirection:")
	fmt.Println("1) Scale UP (high connections)")
	fmt.Println("2) Scale DOWN (low connections)")
	dir := prompt("Select", "1")

	direction := "up"
	if dir == "2" {
		direction = "down"
	}

	threshold := prompt("Average connections threshold", "100")
	scaleCount := prompt("Containers to add/remove", "2")
	cooldown := prompt("Cooldown", "120s")

	name := fmt.Sprintf("conn_%s", direction)
	if direction == "up" {
		name = "high_connections"
	} else {
		name = "low_connections"
	}

	return ScaleTrigger{
		Name:           name,
		Type:           "connection",
		Threshold:      parseFloat(threshold),
		ScaleDirection: direction,
		ScaleCount:     parseInt(scaleCount),
		Cooldown:       cooldown,
	}
}

func trigger429() ScaleTrigger {
	threshold := prompt("429 count threshold", "10")
	window := prompt("Time window", "60s")
	scaleCount := prompt("Containers to add", "1")
	cooldown := prompt("Cooldown", "120s")
	allBackends := promptYesNo("Require all backends affected?", false)

	return ScaleTrigger{
		Name:           "rate_limit",
		Type:           "response_code",
		ResponseCode:   429,
		Threshold:      parseFloat(threshold),
		Window:         window,
		ScaleDirection: "up",
		ScaleCount:     parseInt(scaleCount),
		Cooldown:       cooldown,
		AllBackends:    allBackends,
	}
}

func trigger5xx() ScaleTrigger {
	code := prompt("Response code", "502")
	threshold := prompt("Error count threshold", "20")
	window := prompt("Time window", "60s")
	scaleCount := prompt("Containers to add", "1")
	cooldown := prompt("Cooldown", "120s")

	return ScaleTrigger{
		Name:           "server_error",
		Type:           "response_code",
		ResponseCode:   parseInt(code),
		Threshold:      parseFloat(threshold),
		Window:         window,
		ScaleDirection: "up",
		ScaleCount:     parseInt(scaleCount),
		Cooldown:       cooldown,
	}
}

func promptLoadBalancerConfig() string {
	fmt.Println("\n--- Load Balancer ---")
	choices := []string{"roundrobin (default)", "leastconn", "iphash"}
	choice := promptChoice("Algorithm", choices, 0)

	switch {
	case strings.Contains(choice, "least"):
		return "leastconn"
	case strings.Contains(choice, "ip"):
		return "iphash"
	default:
		return "roundrobin"
	}
}

func promptAuthConfig() (enabled bool, users []string) {
	fmt.Println("\n--- Authentication ---")
	enabled = promptYesNo("Enable SOCKS5 authentication?", false)
	if enabled {
		user := prompt("Username", "admin")
		pass := prompt("Password (stored in plain text)", "")
		if user != "" && pass != "" {
			users = append(users, fmt.Sprintf(`      - user: %s
        pass: "%s"`, user, pass))
		}
	}
	return
}

func promptResourceConfig() (memory, cpu, rotation string) {
	fmt.Println("\n--- Container Resources ---")
	memory = prompt("Memory limit per container", "150m")
	cpu = prompt("CPU limit per container", "0.15")
	rotation = prompt("WARP rotation interval (seconds)", "60")
	return
}

func generateConfig(
	apiPort, socksPort string,
	minContainers, maxContainers, cooldown string,
	triggers []ScaleTrigger,
	algorithm string,
	authEnabled bool,
	authUsers []string,
	memoryLimit, cpuLimit, warpRotation string,
) string {
	// Build triggers YAML
	var triggersYaml string
	if len(triggers) > 0 {
		triggersYaml = "\n  triggers:\n"
		for _, t := range triggers {
			triggersYaml += fmt.Sprintf(`    - name: %s
      type: %s`, t.Name, t.Type)

			if t.ResponseCode > 0 {
				triggersYaml += fmt.Sprintf("\n      response_code: %d", t.ResponseCode)
			}

			triggersYaml += fmt.Sprintf(`
      threshold: %.0f`, t.Threshold)

			if t.Window != "" {
				triggersYaml += fmt.Sprintf("\n      window: %s", t.Window)
			}

			triggersYaml += fmt.Sprintf(`
      scale_direction: %s
      scale_count: %d
      cooldown: %s`, t.ScaleDirection, t.ScaleCount, t.Cooldown)

			if t.AllBackends {
				triggersYaml += "\n      all_backends: true"
			}
			triggersYaml += "\n"
		}
	} else {
		triggersYaml = "\n  triggers: []\n"
	}

	return fmt.Sprintf(`manager:
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
%s
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
`,
		apiPort, socksPort, authEnabled, strings.Join(authUsers, "\n"),
		minContainers, maxContainers, cooldown, triggersYaml,
		algorithm, memoryLimit, cpuLimit, warpRotation,
	)
}

func generateDockerCompose(apiPort, socksPort, dockerSocket, dockerHost, dockerTLSCert string) string {
	if dockerSocket != "" {
		return fmt.Sprintf(`services:
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
	}

	return fmt.Sprintf(`services:
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
      - %s:/certs/client:ro
      - ./config.yaml:/app/config.yaml
      - ./data:/app/data
    networks:
      - warp-net
    restart: unless-stopped

networks:
  warp-net:
    name: warp-net
    driver: bridge
`, apiPort, apiPort, socksPort, socksPort, dockerHost, dockerTLSCert)
}

// Helpers
func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}

func parseInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
