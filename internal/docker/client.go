package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lowkruc/warp-proxy-manager/internal/config"
)

type Client struct {
	config     *config.Config
	httpClient *http.Client
	socketPath string
}

type ContainerInfo struct {
	ID          string
	Name        string
	Status      string
	Port        int
	IP          string
	StartedAt   time.Time
	Connections int64
}

type ContainerCreateResponse struct {
	ID       string   `json:"Id"`
	Warnings []string `json:"Warnings"`
}

type ContainerListResponse struct {
	ID              string            `json:"Id"`
	Names           []string          `json:"Names"`
	State           string            `json:"State"`
	Status          string            `json:"Status"`
	Ports           []PortBinding     `json:"Ports"`
	Created         int64             `json:"Created"`
	Labels          map[string]string `json:"Labels"`
	NetworkSettings ContainerNetwork  `json:"NetworkSettings"`
}

type ContainerNetwork struct {
	Networks map[string]NetworkInfo `json:"Networks"`
}

type PortBinding struct {
	IP          string `json:"Ip"`
	PrivatePort int    `json:"PrivatePort"`
	PublicPort  int    `json:"PublicPort"`
	Type        string `json:"Type"`
}

type ContainerInspectResponse struct {
	ID              string                `json:"Id"`
	Name            string                `json:"Name"`
	State           StateInfo             `json:"State"`
	NetworkSettings NetworkSettings       `json:"NetworkSettings"`
	Created         string                `json:"Created"`
}

type StateInfo struct {
	Status    string `json:"Status"`
	StartedAt string `json:"StartedAt"`
}

type NetworkSettings struct {
	Networks map[string]NetworkInfo `json:"Networks"`
	Ports    map[string][]PortBinding `json:"Ports"`
}

type NetworkInfo struct {
	IPAddress string `json:"IPAddress"`
}

func NewClient(cfg *config.Config) (*Client, error) {
	socketPath := "/var/run/docker.sock"
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		socketPath = os.Getenv("DOCKER_HOST")
		if socketPath == "" {
			socketPath = "unix:///var/run/docker.sock"
		}
	}

	return &Client{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", strings.TrimPrefix(socketPath, "unix://"))
				},
			},
		},
		socketPath: socketPath,
	}, nil
}

func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(jsonBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, "http://localhost"+path, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		var errMsg struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(bodyBytes, &errMsg) == nil && errMsg.Message != "" {
			return nil, fmt.Errorf("docker API %s %s: %d %s", method, path, resp.StatusCode, errMsg.Message)
		}
		return nil, fmt.Errorf("docker API %s %s: %d %s", method, path, resp.StatusCode, string(bodyBytes))
	}

	return bodyBytes, nil
}

func (c *Client) CreateContainer(ctx context.Context, name string) (*ContainerInfo, error) {
	if name == "" {
		name = fmt.Sprintf("%s-%s", c.config.Docker.Prefix, uuid.New().String()[:8])
	}

	port, err := c.findAvailablePort(ctx)
	if err != nil {
		return nil, err
	}

	volName := fmt.Sprintf("%s-data", name)

	env := make([]string, 0, len(c.config.Docker.Env))
	for k, v := range c.config.Docker.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	hostConfig := map[string]interface{}{
		"PortBindings": map[string]interface{}{
			"1080/tcp": []map[string]interface{}{
				{
					"HostIp":   "0.0.0.0",
					"HostPort": strconv.Itoa(port),
				},
			},
		},
		"RestartPolicy": map[string]interface{}{
			"Name": "always",
		},
		"Binds": []string{
			fmt.Sprintf("%s:/var/lib/cloudflare-warp", volName),
		},
		"Sysctls": map[string]interface{}{
			"net.ipv6.conf.all.disable_ipv6":  "0",
			"net.ipv4.conf.all.src_valid_mark": "1",
		},
		"Privileged": true,
	}

	// Add memory limit if configured
	if c.config.Docker.MemoryLimit != "" {
		memBytes, err := parseMemory(c.config.Docker.MemoryLimit)
		if err == nil {
			hostConfig["Memory"] = memBytes
		}
	}

	// Add CPU limit if configured
	if c.config.Docker.CPULimit != "" {
		cpuFloat, err := strconv.ParseFloat(c.config.Docker.CPULimit, 64)
		if err == nil {
			hostConfig["NanoCPUs"] = int64(cpuFloat * 1e9)
		}
	}

	networkConfig := map[string]interface{}{
		"EndpointsConfig": map[string]interface{}{
			c.config.Docker.Network: struct{}{},
		},
	}

	body := map[string]interface{}{
		"Image":           c.config.Docker.Image,
		"Env":             env,
		"ExposedPorts":    map[string]interface{}{"1080/tcp": struct{}{}},
		"HostConfig":      hostConfig,
		"NetworkingConfig": networkConfig,
	}

	// Ensure network
	c.ensureNetwork(ctx)

	// Create container
	respBody, err := c.doRequest(ctx, "POST", fmt.Sprintf("/containers/create?name=%s", name), body)
	if err != nil {
		return nil, fmt.Errorf("create container: %w", err)
	}

	var createResp ContainerCreateResponse
	if err := json.Unmarshal(respBody, &createResp); err != nil {
		return nil, err
	}

	// Start container
	if _, err := c.doRequest(ctx, "POST", fmt.Sprintf("/containers/%s/start", createResp.ID), nil); err != nil {
		return nil, fmt.Errorf("start container: %w", err)
	}

	return &ContainerInfo{
		ID:        truncateID(createResp.ID),
		Name:      name,
		Status:    "starting",
		Port:      port,
		StartedAt: time.Now(),
	}, nil
}

func (c *Client) RemoveContainer(ctx context.Context, id string, force bool) error {
	path := fmt.Sprintf("/containers/%s?force=%t", id, force)
	_, err := c.doRequest(ctx, "DELETE", path, nil)
	return err
}

func (c *Client) RestartContainer(ctx context.Context, id string) error {
	path := fmt.Sprintf("/containers/%s/restart?t=10", id)
	_, err := c.doRequest(ctx, "POST", path, nil)
	return err
}

func (c *Client) ListContainers(ctx context.Context) ([]*ContainerInfo, error) {
	respBody, err := c.doRequest(ctx, "GET", "/containers/json?all=true", nil)
	if err != nil {
		return nil, err
	}

	var containers []ContainerListResponse
	if err := json.Unmarshal(respBody, &containers); err != nil {
		return nil, err
	}

	result := make([]*ContainerInfo, 0)
	for _, cont := range containers {
		name := strings.TrimPrefix(cont.Names[0], "/")
		if !strings.HasPrefix(name, c.config.Docker.Prefix) {
			continue
		}

		port := 0
		if len(cont.Ports) > 0 {
			port = cont.Ports[0].PublicPort
		}

		// Get IP from network settings
		ip := ""
		for _, network := range cont.NetworkSettings.Networks {
			if network.IPAddress != "" {
				ip = network.IPAddress
				break
			}
		}

		info := &ContainerInfo{
			ID:        truncateID(cont.ID),
			Name:      name,
			Status:    cont.State,
			Port:      port,
			IP:        ip,
			StartedAt: time.Unix(cont.Created, 0),
		}
		result = append(result, info)
	}

	return result, nil
}

func (c *Client) GetContainer(ctx context.Context, id string) (*ContainerInfo, error) {
	respBody, err := c.doRequest(ctx, "GET", fmt.Sprintf("/containers/%s/json", id), nil)
	if err != nil {
		return nil, err
	}

	var cont ContainerInspectResponse
	if err := json.Unmarshal(respBody, &cont); err != nil {
		return nil, err
	}

	port := 0
	if ports, ok := cont.NetworkSettings.Ports["1080/tcp"]; ok && len(ports) > 0 {
		port = ports[0].PublicPort
	}

	ip := ""
	if net, ok := cont.NetworkSettings.Networks[c.config.Docker.Network]; ok {
		ip = net.IPAddress
	}

	startedAt := time.Time{}
	if t, err := time.Parse(time.RFC3339Nano, cont.State.StartedAt); err == nil {
		startedAt = t
	}

	return &ContainerInfo{
		ID:        truncateID(cont.ID),
		Name:      strings.TrimPrefix(cont.Name, "/"),
		Status:    cont.State.Status,
		Port:      port,
		IP:        ip,
		StartedAt: startedAt,
	}, nil
}

func (c *Client) RunningCount(ctx context.Context) int {
	containers, _ := c.ListContainers(ctx)
	count := 0
	for _, cont := range containers {
		if cont.Status == "running" {
			count++
		}
	}
	return count
}

func (c *Client) findAvailablePort(ctx context.Context) (int, error) {
	used := make(map[int]bool)
	containers, _ := c.ListContainers(ctx)
	for _, cont := range containers {
		used[cont.Port] = true
	}

	for port := 1081; port < 10000; port++ {
		if !used[port] {
			return port, nil
		}
	}

	return 0, fmt.Errorf("no available port")
}

func (c *Client) ensureNetwork(ctx context.Context) error {
	respBody, err := c.doRequest(ctx, "GET", "/networks", nil)
	if err != nil {
		return err
	}

	var networks []struct {
		Name string `json:"Name"`
	}
	if err := json.Unmarshal(respBody, &networks); err != nil {
		return err
	}

	for _, n := range networks {
		if n.Name == c.config.Docker.Network {
			return nil
		}
	}

	body := map[string]interface{}{
		"Name": c.config.Docker.Network,
		"Driver": "bridge",
	}
	_, err = c.doRequest(ctx, "POST", "/networks/create", body)
	return err
}

func (c *Client) GetContainerLogs(ctx context.Context, id string, tail int) (string, error) {
	respBody, err := c.doRequest(ctx, "GET", fmt.Sprintf("/containers/%s/logs?stdout=true&stderr=true&tail=%d", id, tail), nil)
	if err != nil {
		return "", err
	}
	return string(respBody), nil
}

func (c *Client) Close() error {
	return nil
}

func truncateID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func parseMemory(s string) (int64, error) {
	s = strings.TrimSpace(s)
	s = strings.ToUpper(s)

	var num float64
	var unit string

	// Parse number and unit
	for i, c := range s {
		if c == 'K' || c == 'M' || c == 'G' {
			num, _ = strconv.ParseFloat(s[:i], 64)
			unit = s[i:]
			break
		}
	}
	if unit == "" {
		num, _ = strconv.ParseFloat(s, 64)
		return int64(num), nil
	}

	switch unit {
	case "K":
		return int64(num * 1024), nil
	case "KB":
		return int64(num * 1024), nil
	case "M":
		return int64(num * 1024 * 1024), nil
	case "MB":
		return int64(num * 1024 * 1024), nil
	case "G":
		return int64(num * 1024 * 1024 * 1024), nil
	case "GB":
		return int64(num * 1024 * 1024 * 1024), nil
	default:
		return 0, fmt.Errorf("unknown unit: %s", unit)
	}
}
