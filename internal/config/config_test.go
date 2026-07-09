package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
		check   func(*Config) bool
	}{
		{
			name: "valid config",
			content: `
manager:
  api_port: 8080
  log_level: info
proxy:
  listen: ":1080"
scaling:
  min: 3
  max: 10
docker:
  image: "ghcr.io/lowkruc/warp-proxy:latest"
  memory_limit: "150m"
  cpu_limit: "0.1"
`,
			wantErr: false,
			check: func(c *Config) bool {
				return c.Manager.APIPort == 8080 &&
					c.Scaling.Min == 3 &&
					c.Scaling.Max == 10 &&
					c.Docker.MemoryLimit == "150m" &&
					c.Docker.CPULimit == "0.1"
			},
		},
		{
			name: "empty config uses defaults",
			content: `
manager:
  api_port: 9090
`,
			wantErr: false,
			check: func(c *Config) bool {
				return c.Manager.APIPort == 9090 &&
					c.Proxy.Listen == ":1080" &&
					c.Scaling.Min == 1
			},
		},
		{
			name:    "invalid yaml",
			content: `not: valid: yaml: [`,
			wantErr: true,
		},
		{
			name:    "empty file",
			content: "",
			wantErr: false,
			check: func(c *Config) bool {
				return c.Manager.APIPort == 8080
			},
		},
		{
			name: "auth config",
			content: `
proxy:
  auth:
    enabled: true
    users:
      - user: admin
        pass: secret123
`,
			wantErr: false,
			check: func(c *Config) bool {
				return c.Proxy.Auth.Enabled &&
					len(c.Proxy.Auth.Users) == 1 &&
					c.Proxy.Auth.Users[0].User == "admin" &&
					c.Proxy.Auth.Users[0].Pass == "secret123"
			},
		},
		{
			name: "docker env config",
			content: `
docker:
  env:
    WARP_SLEEP: "5"
    WARP_ROTATION_INTERVAL: "60"
`,
			wantErr: false,
			check: func(c *Config) bool {
				return c.Docker.Env["WARP_SLEEP"] == "5" &&
					c.Docker.Env["WARP_ROTATION_INTERVAL"] == "60"
			},
		},
		{
			name: "timeout config",
			content: `
proxy:
  timeout:
    connect: 10s
    idle: 60s
`,
			wantErr: false,
			check: func(c *Config) bool {
				return c.Proxy.Timeout.Connect == 10*time.Second &&
					c.Proxy.Timeout.Idle == 60*time.Second
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write config: %v", err)
			}

			cfg, err := Load(path)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.check != nil {
				if !tt.check(cfg) {
					t.Errorf("Load() config check failed: %+v", cfg)
				}
			}
		})
	}
}
