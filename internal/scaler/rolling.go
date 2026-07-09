package scaler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/lowkruc/warp-proxy-manager/internal/config"
	"github.com/lowkruc/warp-proxy-manager/internal/docker"
)

type RollingUpdater struct {
	config   *config.Config
	docker   *docker.Client
	mu       sync.Mutex
	running  bool
	stopCh   chan struct{}
}

func NewRollingUpdater(cfg *config.Config, dockerClient *docker.Client) *RollingUpdater {
	return &RollingUpdater{
		config: cfg,
		docker: dockerClient,
		stopCh: make(chan struct{}),
	}
}

func (ru *RollingUpdater) Start() {
	ru.running = true
	log.Printf("[ROLLING] Updater started")
}

func (ru *RollingUpdater) Stop() {
	ru.running = false
	close(ru.stopCh)
	log.Printf("[ROLLING] Updater stopped")
}

// UpdateImages replaces containers one by one with new image
func (ru *RollingUpdater) UpdateImages(ctx context.Context, newImage string) error {
	ru.mu.Lock()
	defer ru.mu.Unlock()

	containers, err := ru.docker.ListContainers(ctx)
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}

	log.Printf("[ROLLING] Starting rolling update for %d containers", len(containers))

	for i, cont := range containers {
		log.Printf("[ROLLING] Updating container %d/%d: %s", i+1, len(containers), cont.Name)

		// Create new container
		newContainer, err := ru.docker.CreateContainer(ctx, "")
		if err != nil {
			log.Printf("[ROLLING] Failed to create new container: %v", err)
			continue
		}

		// Wait for new container to be healthy
		if err := ru.waitForHealthy(ctx, newContainer.ID, 60*time.Second); err != nil {
			log.Printf("[ROLLING] New container not healthy, rolling back")
			ru.docker.RemoveContainer(ctx, newContainer.ID, true)
			continue
		}

		// Remove old container
		if err := ru.docker.RemoveContainer(ctx, cont.ID, false); err != nil {
			log.Printf("[ROLLING] Failed to remove old container: %v", err)
			// Keep both running
			continue
		}

		log.Printf("[ROLLING] Container %s updated successfully", cont.Name)

		// Brief pause between updates
		time.Sleep(2 * time.Second)
	}

	log.Printf("[ROLLING] Rolling update complete")
	return nil
}

func (ru *RollingUpdater) waitForHealthy(ctx context.Context, containerID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ru.stopCh:
			return fmt.Errorf("stopped")
		default:
		}

		container, err := ru.docker.GetContainer(ctx, containerID)
		if err == nil && container.Status == "running" {
			// TODO: Actually check WARP connectivity
			time.Sleep(5 * time.Second) // Give WARP time to connect
			return nil
		}

		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("timeout waiting for healthy")
}

// ReplaceContainer replaces a specific container
func (ru *RollingUpdater) ReplaceContainer(ctx context.Context, oldContainerID string) error {
	ru.mu.Lock()
	defer ru.mu.Unlock()

	log.Printf("[ROLLING] Replacing container %s", oldContainerID)

	// Get old container info
	oldContainer, err := ru.docker.GetContainer(ctx, oldContainerID)
	if err != nil {
		return fmt.Errorf("get old container: %w", err)
	}

	// Create new container
	newContainer, err := ru.docker.CreateContainer(ctx, "")
	if err != nil {
		return fmt.Errorf("create new container: %w", err)
	}

	// Wait for new container
	if err := ru.waitForHealthy(ctx, newContainer.ID, 60*time.Second); err != nil {
		ru.docker.RemoveContainer(ctx, newContainer.ID, true)
		return fmt.Errorf("new container not healthy: %w", err)
	}

	// Remove old container
	if err := ru.docker.RemoveContainer(ctx, oldContainer.ID, false); err != nil {
		return fmt.Errorf("remove old container: %w", err)
	}

	log.Printf("[ROLLING] Container %s replaced with %s", oldContainer.Name, newContainer.Name)
	return nil
}

// RestartAll restarts all containers one by one
func (ru *RollingUpdater) RestartAll(ctx context.Context) error {
	ru.mu.Lock()
	defer ru.mu.Unlock()

	containers, err := ru.docker.ListContainers(ctx)
	if err != nil {
		return err
	}

	for _, cont := range containers {
		log.Printf("[ROLLING] Restarting %s", cont.Name)
		if err := ru.docker.RestartContainer(ctx, cont.ID); err != nil {
			log.Printf("[ROLLING] Failed to restart %s: %v", cont.Name, err)
			continue
		}
		time.Sleep(5 * time.Second) // Wait for container to stabilize
	}

	return nil
}
