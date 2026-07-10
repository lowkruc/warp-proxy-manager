package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Manager      ManagerConfig      `yaml:"manager"`
	Proxy        ProxyConfig        `yaml:"proxy"`
	Scaling      ScalingConfig      `yaml:"scaling"`
	LoadBalancer LoadBalancerConfig `yaml:"loadbalancer"`
	Docker       DockerConfig       `yaml:"docker"`
	Alerting     AlertConfig        `yaml:"alerting"`
}

type AlertConfig struct {
	Enabled bool     `yaml:"enabled"`
	Webhook string   `yaml:"webhook"`
	Events  []string `yaml:"events"` // scale_up, scale_down, unhealthy
}

type ManagerConfig struct {
	APIPort  int    `yaml:"api_port"`
	LogLevel string `yaml:"log_level"`
}

type ProxyConfig struct {
	Listen  string      `yaml:"listen"`
	Auth    AuthConfig  `yaml:"auth"`
	Timeout TimeoutConfig `yaml:"timeout"`
}

type AuthConfig struct {
	Enabled bool     `yaml:"enabled"`
	Users   []User   `yaml:"users"`
}

type User struct {
	User string `yaml:"user"`
	Pass string `yaml:"pass"`
}

type TimeoutConfig struct {
	Connect time.Duration `yaml:"connect"`
	Idle    time.Duration `yaml:"idle"`
}

type ScalingConfig struct {
	Min      int             `yaml:"min"`
	Max      int             `yaml:"max"`
	Cooldown time.Duration   `yaml:"cooldown"`
	Triggers []ScaleTrigger  `yaml:"triggers"`
}

type ScaleTrigger struct {
	Name            string        `yaml:"name"`
	Type            string        `yaml:"type"` // connection | response_code | latency
	Enabled         bool          `yaml:"enabled"`
	ResponseCode    int           `yaml:"response_code,omitempty"`
	Threshold       float64       `yaml:"threshold"`
	Window          time.Duration `yaml:"window"`
	ScaleDirection  string        `yaml:"scale_direction"` // up | down
	ScaleCount      int           `yaml:"scale_count"`
	Cooldown        time.Duration `yaml:"cooldown"`
	AllBackends     bool          `yaml:"all_backends,omitempty"`
}

type LoadBalancerConfig struct {
	Algorithm   string           `yaml:"algorithm"`
	HealthCheck HealthCheckConfig `yaml:"health_check"`
}

type HealthCheckConfig struct {
	Enabled            bool          `yaml:"enabled"`
	Interval           time.Duration `yaml:"interval"`
	Timeout            time.Duration `yaml:"timeout"`
	UnhealthyThreshold int           `yaml:"unhealthy_threshold"`
}

type DockerConfig struct {
	Image       string            `yaml:"image"`
	Network     string            `yaml:"network"`
	Prefix      string            `yaml:"prefix"`
	MemoryLimit string            `yaml:"memory_limit"`
	CPULimit    string            `yaml:"cpu_limit"`
	Volumes     map[string]string `yaml:"volumes"`
	Env         map[string]string `yaml:"env"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func DefaultConfig() *Config {
	return &Config{
		Manager: ManagerConfig{
			APIPort:  8080,
			LogLevel: "info",
		},
		Proxy: ProxyConfig{
			Listen: ":1080",
			Auth: AuthConfig{
				Enabled: true,
			},
			Timeout: TimeoutConfig{
				Connect: 5 * time.Second,
				Idle:    30 * time.Second,
			},
		},
		Scaling: ScalingConfig{
			Min:      1,
			Max:      10,
			Cooldown: 60 * time.Second,
		},
		LoadBalancer: LoadBalancerConfig{
			Algorithm: "roundrobin",
			HealthCheck: HealthCheckConfig{
				Enabled:            true,
				Interval:           10 * time.Second,
				Timeout:            5 * time.Second,
				UnhealthyThreshold: 3,
			},
		},
		Docker: DockerConfig{
			Image:  "ghcr.io/lowkruc/warp-proxy:latest",
			Network: "warp-net",
			Prefix: "warp",
		},
	}
}
