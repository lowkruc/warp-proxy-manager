package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

func cmdSet(args []string) {
	dir := ensureInitialized()

	if len(args) == 0 {
		printSetUsage()
		os.Exit(1)
	}

	resource := args[0]
	switch resource {
	case "user":
		cmdSetUser(dir, args[1:])
	case "loadbalancer", "lb":
		cmdSetLoadBalancer(dir, args[1:])
	case "auth":
		cmdSetAuth(dir, args[1:])
	case "scaling":
		cmdSetScaling(dir, args[1:])
	default:
		fmt.Printf("Unknown resource: %s\n", resource)
		printSetUsage()
		os.Exit(1)
	}
}

func printSetUsage() {
	fmt.Println(`
Usage: warpctl set <resource> [args...]

Resources:
  user <add|remove|password|list>  Manage users
  loadbalancer <algo>              Set LB algorithm (roundrobin|leastconn|iphash)
  auth <on|off>                    Enable/disable SOCKS5 auth
  scaling <min|max> <n>            Set scaling limits

Examples:
  warpctl set user add admin mypassword
  warpctl set user remove admin
  warpctl set user password admin newpass123
  warpctl set user list
  warpctl set loadbalancer leastconn
  warpctl set auth on
  warpctl set scaling min 3
  warpctl set scaling max 10`)
}

// ── User management ──────────────────────────────────────────

func cmdSetUser(dir string, args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: warpctl set user <add|remove|password|list> [username] [password]")
		os.Exit(1)
	}

	action := args[0]
	switch action {
	case "list":
		users := readUsersFromConfig(dir)
		if len(users) == 0 {
			fmt.Println("No users configured")
			return
		}
		fmt.Println("Users:")
		for _, u := range users {
			fmt.Printf("  - %s\n", u)
		}

	case "add":
		if len(args) < 3 {
			fmt.Println("Usage: warpctl set user add <username> <password>")
			os.Exit(1)
		}
		addUser(dir, args[1], args[2])

	case "remove":
		if len(args) < 2 {
			fmt.Println("Usage: warpctl set user remove <username>")
			os.Exit(1)
		}
		removeUser(dir, args[1])

	case "password":
		if len(args) < 3 {
			fmt.Println("Usage: warpctl set user password <username> <new_password>")
			os.Exit(1)
		}
		changePassword(dir, args[1], args[2])

	default:
		fmt.Printf("Unknown action: %s\n", action)
		fmt.Println("Usage: warpctl set user <add|remove|password|list> [username] [password]")
		os.Exit(1)
	}
}

func readUsersFromConfig(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		return nil
	}

	lines := strings.Split(string(data), "\n")
	var users []string
	inUsers := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "users:" {
			inUsers = true
			continue
		}
		if inUsers && strings.HasPrefix(trimmed, "- user:") {
			user := strings.TrimSpace(strings.TrimPrefix(trimmed, "- user:"))
			users = append(users, user)
		}
		if inUsers && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && trimmed != "" && !strings.HasPrefix(trimmed, "- user:") {
			break
		}
	}
	return users
}

func addUser(dir, username, password string) {
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		fmt.Printf("Error reading config: %v\n", err)
		os.Exit(1)
	}

	content := string(data)

	// Check if user already exists
	users := readUsersFromConfig(dir)
	for _, u := range users {
		if u == username {
			fmt.Printf("User '%s' already exists\n", username)
			os.Exit(1)
		}
	}

	// Ensure auth is enabled
	if !strings.Contains(content, "enabled: true") {
		content = enableAuth(content)
	}

	newEntry := "      - user: " + username + "\n        pass: \"" + password + "\""
	lines := strings.Split(content, "\n")
	var result []string
	inserted := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Handle "users: []" → replace with user list
		if trimmed == "users: []" {
			result = append(result, "    users:")
			result = append(result, newEntry)
			inserted = true
			continue
		}

		// Handle "users:" with entries below
		if trimmed == "users:" && !inserted {
			result = append(result, line)
			// If next line starts with "- user:", we'll insert after last user
			// Otherwise insert now (empty block)
			if i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "- user:") {
				continue
			}
			result = append(result, newEntry)
			inserted = true
			continue
		}

		// Insert after last user entry (before next non-user block)
		if !inserted && i > 0 {
			prevTrimmed := strings.TrimSpace(lines[i-1])
			if strings.HasPrefix(prevTrimmed, "- user:") && !strings.HasPrefix(trimmed, "pass:") {
				result = append(result, newEntry)
				inserted = true
			}
		}

		result = append(result, line)
	}

	if !inserted {
		result = append(result, newEntry)
	}

	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(strings.Join(result, "\n")), 0644); err != nil {
		fmt.Printf("Error writing config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ User '%s' added\n", username)
	applyConfig(dir, "user added")
}

func removeUser(dir, username string) {
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		fmt.Printf("Error reading config: %v\n", err)
		os.Exit(1)
	}

	lines := strings.Split(string(data), "\n")
	var result []string
	skipNext := false
	found := false

	for i, line := range lines {
		if skipNext {
			skipNext = false
			continue
		}

		trimmed := strings.TrimSpace(line)

		// Match "- user: username"
		if strings.HasPrefix(trimmed, "- user:") {
			user := strings.TrimSpace(strings.TrimPrefix(trimmed, "- user:"))
			if user == username {
				found = true
				// Skip pass line too
				if i+1 < len(lines) && strings.Contains(lines[i+1], "pass:") {
					skipNext = true
				}
				continue
			}
		}

		result = append(result, line)
	}

	if !found {
		fmt.Printf("User '%s' not found\n", username)
		os.Exit(1)
	}

	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(strings.Join(result, "\n")), 0644); err != nil {
		fmt.Printf("Error writing config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ User '%s' removed\n", username)
	applyConfig(dir, "user removed")
}

func changePassword(dir, username, newPassword string) {
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		fmt.Printf("Error reading config: %v\n", err)
		os.Exit(1)
	}

	lines := strings.Split(string(data), "\n")
	var result []string
	found := false
	skipNext := false

	for i, line := range lines {
		if skipNext {
			skipNext = false
			continue
		}

		trimmed := strings.TrimSpace(line)

		// Found user line
		if strings.HasPrefix(trimmed, "- user:") {
			user := strings.TrimSpace(strings.TrimPrefix(trimmed, "- user:"))
			if user == username {
				found = true
				result = append(result, line)
				// Replace next line (pass)
				if i+1 < len(lines) && strings.Contains(lines[i+1], "pass:") {
					passLine := lines[i+1]
					passIdx := strings.Index(passLine, "pass:")
					if passIdx >= 0 {
						indent := passLine[:passIdx]
						result = append(result, indent+"pass: \""+newPassword+"\"")
					} else {
						result = append(result, passLine)
					}
					skipNext = true
				}
				continue
			}
		}

		result = append(result, line)
	}

	if !found {
		fmt.Printf("User '%s' not found\n", username)
		os.Exit(1)
	}

	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(strings.Join(result, "\n")), 0644); err != nil {
		fmt.Printf("Error writing config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Password changed for user '%s'\n", username)
	applyConfig(dir, "password changed")
}

// ── Load balancer ────────────────────────────────────────────

func cmdSetLoadBalancer(dir string, args []string) {
	if len(args) == 0 {
		data, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
		algo := extractValue(string(data), "algorithm:")
		if algo == "" {
			algo = "roundrobin"
		}
		fmt.Printf("Current: %s\n", algo)
		fmt.Println("Options: roundrobin, leastconn, iphash")
		return
	}

	algo := args[0]
	valid := map[string]bool{"roundrobin": true, "leastconn": true, "iphash": true}
	if !valid[algo] {
		fmt.Printf("Invalid algorithm: %s\n", algo)
		fmt.Println("Options: roundrobin, leastconn, iphash")
		os.Exit(1)
	}

	updateConfigValue(dir, "algorithm:", algo)
	fmt.Printf("✓ Load balancer set to '%s'\n", algo)
	applyConfig(dir, "load balancer changed")
}

// ── Auth ─────────────────────────────────────────────────────

func cmdSetAuth(dir string, args []string) {
	if len(args) == 0 {
		data, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
		enabled := extractValue(string(data), "enabled:")
		if enabled == "true" {
			fmt.Println("Auth: enabled")
		} else {
			fmt.Println("Auth: disabled")
		}
		return
	}

	switch args[0] {
	case "on", "true", "enable":
		enableAuthConfig(dir)
		fmt.Println("✓ Auth enabled")
	case "off", "false", "disable":
		disableAuthConfig(dir)
		fmt.Println("✓ Auth disabled")
	default:
		fmt.Printf("Unknown option: %s\n", args[0])
		fmt.Println("Usage: warpctl set auth <on|off>")
		os.Exit(1)
	}
	applyConfig(dir, "auth changed")
}

func enableAuthConfig(dir string) {
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		fmt.Printf("Error reading config: %v\n", err)
		os.Exit(1)
	}

	content := enableAuth(string(data))
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0644); err != nil {
		fmt.Printf("Error writing config: %v\n", err)
		os.Exit(1)
	}
}

func enableAuth(content string) string {
	lines := strings.Split(content, "\n")
	var result []string
	inAuth := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "auth:" {
			inAuth = true
		}
		if inAuth && strings.HasPrefix(trimmed, "enabled:") {
			result = append(result, strings.Replace(line, "enabled: false", "enabled: true", 1))
			inAuth = false
			continue
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

func disableAuthConfig(dir string) {
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		fmt.Printf("Error reading config: %v\n", err)
		os.Exit(1)
	}

	lines := strings.Split(string(data), "\n")
	var result []string
	inAuth := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "auth:" {
			inAuth = true
		}
		if inAuth && strings.HasPrefix(trimmed, "enabled:") {
			result = append(result, strings.Replace(line, "enabled: true", "enabled: false", 1))
			inAuth = false
			continue
		}
		result = append(result, line)
	}

	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(strings.Join(result, "\n")), 0644); err != nil {
		fmt.Printf("Error writing config: %v\n", err)
		os.Exit(1)
	}
}

// ── Scaling ──────────────────────────────────────────────────

func cmdSetScaling(dir string, args []string) {
	if len(args) < 2 {
		data, _ := os.ReadFile(filepath.Join(dir, "config.yaml"))
		min := extractValue(string(data), "min:")
		max := extractValue(string(data), "max:")
		fmt.Printf("Scaling: min=%s, max=%s\n", min, max)
		fmt.Println("Usage: warpctl set scaling <min|max> <n>")
		return
	}

	key := args[0]
	value := args[1]

	switch key {
	case "min":
		updateConfigValue(dir, "min:", value)
		fmt.Printf("✓ Scaling min set to %s\n", value)
	case "max":
		updateConfigValue(dir, "max:", value)
		fmt.Printf("✓ Scaling max set to %s\n", value)
	default:
		fmt.Printf("Unknown key: %s\n", key)
		fmt.Println("Usage: warpctl set scaling <min|max> <n>")
		os.Exit(1)
	}
	applyConfig(dir, "scaling changed")
}

// ── Config helpers ───────────────────────────────────────────

func updateConfigValue(dir, key, value string) {
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		fmt.Printf("Error reading config: %v\n", err)
		os.Exit(1)
	}

	lines := strings.Split(string(data), "\n")
	var result []string
	updated := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key) && !updated {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			result = append(result, indent+key+" "+value)
			updated = true
			continue
		}
		result = append(result, line)
	}

	if !updated {
		fmt.Printf("Key '%s' not found in config\n", key)
		os.Exit(1)
	}

	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(strings.Join(result, "\n")), 0644); err != nil {
		fmt.Printf("Error writing config: %v\n", err)
		os.Exit(1)
	}
}

func extractValue(content, key string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key) {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, key))
			val = strings.Trim(val, `"`)
			return val
		}
	}
	return ""
}

// ── Apply config to running containers ───────────────────────

func applyConfig(dir, reason string) {
	listCmd := exec.Command("docker", "compose", "-f", filepath.Join(dir, "docker-compose.yml"), "ps", "-q")
	output, _ := listCmd.Output()
	if len(strings.TrimSpace(string(output))) == 0 {
		fmt.Println("  (containers not running, will apply on next start)")
		return
	}

	fmt.Printf("  Applying (%s)...\n", reason)
	restartCmd := exec.Command("docker", "compose", "-f", filepath.Join(dir, "docker-compose.yml"), "restart")
	restartCmd.Stdout = os.Stdout
	restartCmd.Stderr = os.Stderr
	if err := restartCmd.Run(); err != nil {
		fmt.Printf("  Warning: restart failed: %v\n", err)
		return
	}
	fmt.Println("  ✓ Containers restarted")
}

// ── Config value extraction with regex ───────────────────────

func extractConfigInt(content, key string) int {
	re := regexp.MustCompile(key + `\s*(\d+)`)
	matches := re.FindStringSubmatch(content)
	if len(matches) < 2 {
		return 0
	}
	val := 0
	fmt.Sscanf(matches[1], "%d", &val)
	return val
}
