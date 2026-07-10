package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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

func cmdUninstall() {
	ensureInitialized()

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

func ensureInitialized() {
	if _, err := os.Stat(filepath.Join(InstallDir, "docker-compose.yml")); os.IsNotExist(err) {
		fmt.Println("Not initialized. Run 'warpctl init' first.")
		os.Exit(1)
	}
}
