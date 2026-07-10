package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func cmdStart() {
	dir := ensureInitialized()

	fmt.Println("Starting Warp Proxy Manager...")
	cmd := exec.Command("docker", "compose", "-f", filepath.Join(dir, "docker-compose.yml"), "up", "-d")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Error starting: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Started")
	fmt.Println()

	// Read ports from config
	apiPort, socksPort := readPortsFromConfig(dir)
	fmt.Printf("API: http://localhost:%s\n", apiPort)
	fmt.Printf("SOCKS5: localhost:%s\n", socksPort)
}

func readPortsFromConfig(dir string) (apiPort, socksPort string) {
	apiPort = "8080"
	socksPort = "1080"

	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		return
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "api_port:") {
			apiPort = strings.TrimSpace(strings.TrimPrefix(line, "api_port:"))
		} else if strings.HasPrefix(line, "listen:") {
			socksPort = strings.TrimSpace(strings.TrimPrefix(line, "listen:"))
			socksPort = strings.Trim(socksPort, `\"	 :`)
		}
	}
	return
}

func cmdStop() {
	dir := ensureInitialized()

	fmt.Println("Stopping Warp Proxy Manager...")
	cmd := exec.Command("docker", "compose", "-f", filepath.Join(dir, "docker-compose.yml"), "down")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("Error stopping: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Stopped")
}

func cmdUpdate() {
	dir := ensureInitialized()

	fmt.Println("Updating Warp Proxy Manager...")

	// Step 1: Update warpctl binary
	fmt.Println("\n[1/3] Updating warpctl binary...")
	updateBinary()

	// Step 2: Pull latest images
	fmt.Println("\n[2/3] Pulling images...")
	pullImages(dir)

	// Step 3: Recreate containers
	fmt.Println("\n[3/3] Recreating containers...")
	upCmd := exec.Command("docker", "compose", "-f", filepath.Join(dir, "docker-compose.yml"), "up", "-d", "--force-recreate")
	upCmd.Stdout = os.Stdout
	upCmd.Stderr = os.Stderr
	if err := upCmd.Run(); err != nil {
		fmt.Printf("Error recreating: %v\n", err)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("✓ Updated")
}

func updateBinary() {
	repo := "lowkruc/warp-proxy-manager"
	installPath := "/usr/local/bin/warpctl"

	// Detect OS and arch
	osName := strings.ToLower(strings.TrimSpace(mustRun("uname", "-s")))
	archName := strings.TrimSpace(mustRun("uname", "-m"))
	switch archName {
	case "x86_64":
		archName = "amd64"
	case "aarch64", "arm64":
		archName = "arm64"
	case "armv7l":
		archName = "armv7"
	default:
		fmt.Printf("Unsupported arch: %s, skipping binary update\n", archName)
		return
	}

	// Get latest release tag
	latestURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	resp, err := http.Get(latestURL)
	if err != nil {
		fmt.Printf("Warning: cannot check for updates: %v\n", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var release struct {
		TagName string `json:"tag_name"`
	}
	json.Unmarshal(body, &release)

	if release.TagName == "" {
		fmt.Println("Warning: no releases found, skipping binary update")
		return
	}

	// Get current version
	currentVersion := strings.TrimSpace(mustRun(installPath, "version"))
	// Handle both "0.2.0" and "v0.2.0" formats
	currentVer := strings.TrimPrefix(currentVersion, "warpctl ")
	currentVer = strings.TrimPrefix(currentVer, "v")
	latestVer := strings.TrimPrefix(release.TagName, "v")
	if currentVer == latestVer {
		fmt.Printf("Already up to date (%s)\n", release.TagName)
		return
	}

	// Download new binary
	downloadURL := fmt.Sprintf("https://github.com/%s/releases/download/%s/warpctl-%s-%s",
		repo, release.TagName, osName, archName)

	fmt.Printf("Downloading %s...\n", release.TagName)
	tmpFile := "/tmp/warpctl-update"
	if err := downloadFile(downloadURL, tmpFile); err != nil {
		fmt.Printf("Warning: download failed: %v\n", err)
		return
	}

	// Replace binary
	os.Chmod(tmpFile, 0755)
	if err := os.Rename(tmpFile, installPath); err != nil {
		// Try with sudo
		fmt.Println("Trying with sudo...")
		exec.Command("sudo", "mv", tmpFile, installPath).Run()
	}

	fmt.Printf("✓ warpctl %s → %s\n", currentVersion, release.TagName)
}

func pullImages(dir string) {
	// Pull manager image
	managerImage := "ghcr.io/lowkruc/warp-proxy-manager:latest"
	fmt.Printf("Pulling %s...\n", managerImage)
	pullCmd := exec.Command("docker", "pull", managerImage)
	pullCmd.Stdout = os.Stdout
	pullCmd.Stderr = os.Stderr
	_ = pullCmd.Run()

	// Pull proxy image from config
	image := readImageFromConfig(dir)
	if image == "" {
		image = "ghcr.io/lowkruc/warp-proxy:latest"
	}
	fmt.Printf("Pulling %s...\n", image)
	pullCmd2 := exec.Command("docker", "pull", image)
	pullCmd2.Stdout = os.Stdout
	pullCmd2.Stderr = os.Stderr
	_ = pullCmd2.Run()
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func mustRun(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func readImageFromConfig(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil {
		return ""
	}

	lines := strings.Split(string(data), "\n")
	inDocker := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "docker:" {
			inDocker = true
			continue
		}
		if inDocker && strings.HasPrefix(trimmed, "image:") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "image:"))
			val = strings.Trim(val, `"`)
			return val
		}
		if inDocker && !strings.HasPrefix(trimmed, " ") && trimmed != "" && !strings.HasPrefix(trimmed, "image:") {
			break
		}
	}
	return ""
}

func cmdUninstall() {
	dir := ensureInitialized()

	fmt.Println("═══════════════════════════════════════")
	fmt.Println("   Warp Proxy Manager Uninstall")
	fmt.Println("═══════════════════════════════════════")
	fmt.Println()

	// Confirm
	if !promptYesNo("This will remove all containers and config. Continue?", false) {
		fmt.Println("Cancelled.")
		return
	}

	// Stop containers
	fmt.Println("\nStopping containers...")
	cmd := exec.Command("docker", "compose", "-f", filepath.Join(dir, "docker-compose.yml"), "down", "-v")
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
	fmt.Printf("Removing %s...\n", dir)
	os.RemoveAll(dir)

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

func findInstallDir() string {
	// Try /opt first
	if _, err := os.Stat(filepath.Join(InstallDir, "docker-compose.yml")); err == nil {
		return InstallDir
	}

	// Try ~/.warp-proxy-manager
	home, _ := os.UserHomeDir()
	userDir := filepath.Join(home, ".warp-proxy-manager")
	if _, err := os.Stat(filepath.Join(userDir, "docker-compose.yml")); err == nil {
		return userDir
	}

	return ""
}

func ensureInitialized() string {
	dir := findInstallDir()
	if dir == "" {
		fmt.Println("Not initialized. Run 'warpctl init' first.")
		os.Exit(1)
	}
	return dir
}
