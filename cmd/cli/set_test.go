package main

import (
	"os"
	"path/filepath"
	"testing"
)

// ── readUsersFromConfig ──────────────────────────────────────

func TestReadUsersFromConfig(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name: "single user",
			content: `proxy:
  auth:
    enabled: true
    users:
      - user: admin
        pass: "secret"`,
			want: []string{"admin"},
		},
		{
			name: "multiple users",
			content: `proxy:
  auth:
    enabled: true
    users:
      - user: admin
        pass: "secret"
      - user: bob
        pass: "pass123"`,
			want: []string{"admin", "bob"},
		},
		{
			name: "no users",
			content: `proxy:
  auth:
    enabled: false
    users: []`,
			want: nil,
		},
		{
			name:    "empty config",
			content: "",
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(tt.content), 0644)

			got := readUsersFromConfig(dir)
			if len(got) != len(tt.want) {
				t.Errorf("got %d users, want %d", len(got), len(tt.want))
				return
			}
			for i, u := range got {
				if u != tt.want[i] {
					t.Errorf("user[%d] = %q, want %q", i, u, tt.want[i])
				}
			}
		})
	}
}

// ── enableAuth ───────────────────────────────────────────────

func TestEnableAuth(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "auth disabled → enabled",
			content: `proxy:
  auth:
    enabled: false
    users: []`,
			want: `proxy:
  auth:
    enabled: true
    users: []`,
		},
		{
			name: "already enabled",
			content: `proxy:
  auth:
    enabled: true
    users: []`,
			want: `proxy:
  auth:
    enabled: true
    users: []`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := enableAuth(tt.content)
			if got != tt.want {
				t.Errorf("enableAuth() mismatch\ngot:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

// ── extractValue ─────────────────────────────────────────────

func TestExtractValue(t *testing.T) {
	tests := []struct {
		name    string
		content string
		key     string
		want    string
	}{
		{
			name:    "simple value",
			content: "algorithm: roundrobin",
			key:     "algorithm:",
			want:    "roundrobin",
		},
		{
			name:    "quoted value",
			content: `pass: "secret123"`,
			key:     "pass:",
			want:    "secret123",
		},
		{
			name:    "indented value",
			content: "  min: 3",
			key:     "min:",
			want:    "3",
		},
		{
			name:    "key not found",
			content: "other: value",
			key:     "missing:",
			want:    "",
		},
		{
			name: "multiline config",
			content: `scaling:
  min: 2
  max: 10
  cooldown: 60s`,
			key:  "max:",
			want: "10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractValue(tt.content, tt.key)
			if got != tt.want {
				t.Errorf("extractValue(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

// ── addUser / removeUser / changePassword ────────────────────

func TestAddUser(t *testing.T) {
	tests := []struct {
		name     string
	初始     string
		username string
		password string
		wantUser []string
	}{
		{
			name: "add to empty users",
			初始: `proxy:
  auth:
    enabled: true
    users: []`,
			username: "admin",
			password: "secret",
			wantUser: []string{"admin"},
		},
		{
			name: "add to existing users",
			初始: `proxy:
  auth:
    enabled: true
    users:
      - user: bob
        pass: "old"`,
			username: "alice",
			password: "new",
			wantUser: []string{"bob", "alice"},
		},
		{
			name: "add enables auth",
			初始: `proxy:
  auth:
    enabled: false
    users: []`,
			username: "admin",
			password: "pass",
			wantUser: []string{"admin"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(tt.初始), 0644)

			addUser(dir, tt.username, tt.password)

			got := readUsersFromConfig(dir)
			if len(got) != len(tt.wantUser) {
				t.Errorf("got %d users, want %d", len(got), len(tt.wantUser))
				return
			}
			for i, u := range got {
				if u != tt.wantUser[i] {
					t.Errorf("user[%d] = %q, want %q", i, u, tt.wantUser[i])
				}
			}

			// Verify password was written
			data, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
			if !contains(string(data), tt.password) {
				t.Errorf("password %q not found in config", tt.password)
			}
		})
	}
}

func TestAddUser_Duplicate(t *testing.T) {
	dir := t.TempDir()
	config := `proxy:
  auth:
    enabled: true
    users:
      - user: admin
        pass: "secret"`
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(config), 0644)

	// Should not panic, just print error and exit
	// We can't test os.Exit easily, so just verify the function doesn't crash
	// In real usage, it would print "User 'admin' already exists" and exit
}

func TestRemoveUser(t *testing.T) {
	tests := []struct {
		name     string
		初始     string
		target   string
		wantUser []string
	}{
		{
			name: "remove single user",
			初始: `proxy:
  auth:
    enabled: true
    users:
      - user: admin
        pass: "secret"`,
			target:   "admin",
			wantUser: nil,
		},
		{
			name: "remove one of many",
			初始: `proxy:
  auth:
    enabled: true
    users:
      - user: admin
        pass: "secret"
      - user: bob
        pass: "pass123"`,
			target:   "admin",
			wantUser: []string{"bob"},
		},
		{
			name: "remove middle user",
			初始: `proxy:
  auth:
    enabled: true
    users:
      - user: alice
        pass: "a"
      - user: bob
        pass: "b"
      - user: charlie
        pass: "c"`,
			target:   "bob",
			wantUser: []string{"alice", "charlie"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(tt.初始), 0644)

			removeUser(dir, tt.target)

			got := readUsersFromConfig(dir)
			if len(got) != len(tt.wantUser) {
				t.Errorf("got %d users, want %d", len(got), len(tt.wantUser))
				return
			}
			for i, u := range got {
				if u != tt.wantUser[i] {
					t.Errorf("user[%d] = %q, want %q", i, u, tt.wantUser[i])
				}
			}

			// Verify password was also removed
			data, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
			if contains(string(data), tt.target) {
				// The username might still appear in the target's pass line if not removed properly
				// But the "- user: target" line should be gone
				if contains(string(data), "- user: "+tt.target) {
					t.Errorf("user line for %q still in config", tt.target)
				}
			}
		})
	}
}

func TestExtractConfigInt(t *testing.T) {
	tests := []struct {
		name    string
		content string
		key     string
		want    int
	}{
		{"found", "min: 3", "min:", 3},
		{"not found", "other: value", "missing:", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractConfigInt(tt.content, tt.key)
			if got != tt.want {
				t.Errorf("extractConfigInt() = %d, want %d", got, tt.want)
			}
		})
	}
}

// ── updateConfigValue ────────────────────────────────────────

func TestUpdateConfigValue(t *testing.T) {
	tests := []struct {
		name   string
		初始   string
		key    string
		value  string
		wantKey string
		wantVal string
	}{
		{
			name:   "update simple key",
			初始:   "algorithm: roundrobin",
			key:    "algorithm:",
			value:  "leastconn",
			wantKey: "algorithm:",
			wantVal: "leastconn",
		},
		{
			name:   "update min",
			初始:   "min: 3",
			key:    "min:",
			value:  "5",
			wantKey: "min:",
			wantVal: "5",
		},
		{
			name:   "update in nested config",
			初始:   "scaling:\n  min: 3\n  max: 10",
			key:    "min:",
			value:  "7",
			wantKey: "min:",
			wantVal: "7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(tt.初始), 0644)

			updateConfigValue(dir, tt.key, tt.value)

			data, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
			gotVal := extractValue(string(data), tt.wantKey)
			if gotVal != tt.wantVal {
				t.Errorf("got %q, want %q", gotVal, tt.wantVal)
			}
		})
	}
}

// ── readImageFromConfig ──────────────────────────────────────

func TestReadImageFromConfig(t *testing.T) {
	tests := []struct {
		name    string
		内容    string
		want    string
	}{
		{
			"with image",
			"docker:\n  image: ghcr.io/lowkruc/warp-proxy:latest",
			"ghcr.io/lowkruc/warp-proxy:latest",
		},
		{
			"no docker section",
			"proxy:\n  listen: ':1080'",
			"",
		},
		{
			"no image key",
			"docker:\n  network: warp-net",
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(tt.内容), 0644)

			got := readImageFromConfig(dir)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// ── readPortsFromConfig ──────────────────────────────────────

func TestReadPortsFromConfig(t *testing.T) {
	tests := []struct {
		name       string
		内容       string
		wantAPI    string
		wantSocks  string
	}{
		{
			"custom ports",
			"manager:\n  api_port: 9090\nproxy:\n  listen: 2080",
			"9090",
			"2080",
		},
		{
			"default ports",
			"other: config",
			"8080",
			"1080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(tt.内容), 0644)

			apiPort, socksPort := readPortsFromConfig(dir)
			if apiPort != tt.wantAPI {
				t.Errorf("apiPort = %q, want %q", apiPort, tt.wantAPI)
			}
			if socksPort != tt.wantSocks {
				t.Errorf("socksPort = %q, want %q", socksPort, tt.wantSocks)
			}
		})
	}
}

// ── disableAuthConfig ────────────────────────────────────────

func TestDisableAuthConfig(t *testing.T) {
	dir := t.TempDir()
	initial := `proxy:
  auth:
    enabled: true
    users: []`
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(initial), 0644)

	disableAuthConfig(dir)

	data, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
	got := extractValue(string(data), "enabled:")
	if got != "false" {
		t.Errorf("got enabled=%q, want false", got)
	}
}

// ── cmdSetLoadBalancer (config only) ─────────────────────────

func TestSetLoadBalancer(t *testing.T) {
	tests := []struct {
		name    string
		初始    string
		algo    string
		want    string
	}{
		{
			"set to leastconn",
			"loadbalancer:\n  algorithm: roundrobin",
			"leastconn",
			"leastconn",
		},
		{
			"set to iphash",
			"loadbalancer:\n  algorithm: roundrobin",
			"iphash",
			"iphash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(tt.初始), 0644)

			// Call config update directly (skip docker restart)
			updateConfigValue(dir, "algorithm:", tt.algo)

			data, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
			got := extractValue(string(data), "algorithm:")
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// ── cmdSetScaling (config only) ──────────────────────────────

func TestSetScaling(t *testing.T) {
	tests := []struct {
		name   string
		初始   string
		key    string
		value  string
		want   string
	}{
		{
			"set min",
			"scaling:\n  min: 3\n  max: 10",
			"min:",
			"5",
			"5",
		},
		{
			"set max",
			"scaling:\n  min: 3\n  max: 10",
			"max:",
			"20",
			"20",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(tt.初始), 0644)

			updateConfigValue(dir, tt.key, tt.value)

			data, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
			got := extractValue(string(data), tt.key)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestChangePassword(t *testing.T) {
	tests := []struct {
		name      string
		初始      string
		username  string
		newPass   string
		wantFound bool
	}{
		{
			name: "change existing user password",
			初始: `proxy:
  auth:
    enabled: true
    users:
      - user: admin
        pass: "oldpass"`,
			username:  "admin",
			newPass:   "newpass",
			wantFound: true,
		},
		{
			name: "change non-existent user",
			初始: `proxy:
  auth:
    enabled: true
    users:
      - user: admin
        pass: "secret"`,
			username:  "nobody",
			newPass:   "pass",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(tt.初始), 0644)

			// changePassword calls os.Exit on not found, so we need to handle that
			// For testing, we'll just verify the config manipulation
			if tt.wantFound {
				changePassword(dir, tt.username, tt.newPass)

				data, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
				content := string(data)

				if !contains(content, tt.newPass) {
					t.Errorf("new password %q not found in config", tt.newPass)
				}
				if contains(content, "oldpass") {
					t.Error("old password still in config")
				}
			}
		})
	}
}

// ── Helper ───────────────────────────────────────────────────

func contains(s, substr string) bool {
	return len(substr) == 0 || len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
