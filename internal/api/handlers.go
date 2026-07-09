package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lowkruc/warp-proxy-manager/internal/config"
	"github.com/lowkruc/warp-proxy-manager/internal/docker"
	"github.com/lowkruc/warp-proxy-manager/internal/proxy"
	"github.com/lowkruc/warp-proxy-manager/internal/scaler"
	"github.com/lowkruc/warp-proxy-manager/internal/store"
)

type Handler struct {
	config    *config.Config
	docker    *docker.Client
	balancer  *proxy.LoadBalancer
	scaler    *scaler.Scaler
	proxy     *proxy.ProxyServer
	store     *store.Store
	metrics   *store.MetricsCollector
}

func NewHandler(cfg *config.Config, dockerClient *docker.Client, balancer *proxy.LoadBalancer, scaler *scaler.Scaler, proxyServer *proxy.ProxyServer, st *store.Store, mc *store.MetricsCollector) *Handler {
	return &Handler{
		config:   cfg,
		docker:   dockerClient,
		balancer: balancer,
		scaler:   scaler,
		proxy:    proxyServer,
		store:    st,
		metrics:  mc,
	}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	api := r.Group("/api/v1")

	// Proxy
	api.GET("/proxy/stats", h.getProxyStats)
	api.GET("/proxy/connections", h.getConnections)

	// Containers
	api.GET("/containers", h.listContainers)
	api.GET("/containers/:id", h.getContainer)
	api.POST("/containers", h.createContainer)
	api.DELETE("/containers/:id", h.deleteContainer)
	api.POST("/containers/:id/restart", h.restartContainer)

	// Scaling
	api.GET("/scaling", h.getScalingConfig)
	api.PUT("/scaling", h.updateScalingConfig)
	api.POST("/scaling/scale/:count", h.manualScale)
	api.GET("/scaling/history", h.getScaleHistory)

	// Health
	api.GET("/health", h.getHealth)
	api.GET("/health/containers", h.getContainerHealth)

	// Metrics
	api.GET("/metrics", h.getMetrics)
	api.GET("/metrics/history", h.getMetricsHistory)

	// Config
	api.GET("/config", h.getConfig)
	api.PUT("/config", h.updateConfig)

	// Prometheus metrics
	r.GET("/metrics/prometheus", h.getPrometheusMetrics)
}

func (h *Handler) getProxyStats(c *gin.Context) {
	stats := h.proxy.GetStats()
	c.JSON(http.StatusOK, gin.H{
		"total_requests":   stats.TotalRequests,
		"total_429":        stats.Total429,
		"active_connections": stats.ActiveConns,
		"per_backend":      stats.PerBackend,
		"backends_healthy": stats.BackendsHealthy,
	})
}

func (h *Handler) getConnections(c *gin.Context) {
	stats := h.proxy.GetStats()
	c.JSON(http.StatusOK, gin.H{
		"total":       stats.ActiveConns,
		"per_backend": stats.PerBackend,
	})
}

func (h *Handler) listContainers(c *gin.Context) {
	containers, err := h.docker.ListContainers(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result := make([]gin.H, len(containers))
	for i, cont := range containers {
		result[i] = gin.H{
			"id":          cont.ID,
			"name":        cont.Name,
			"status":      cont.Status,
			"port":        cont.Port,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"containers": result,
		"total":      len(result),
	})
}

func (h *Handler) getContainer(c *gin.Context) {
	id := c.Param("id")
	container, err := h.docker.GetContainer(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Container not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":         container.ID,
		"name":       container.Name,
		"status":     container.Status,
		"port":       container.Port,
		"ip":         container.IP,
		"started_at": container.StartedAt,
	})
}

func (h *Handler) createContainer(c *gin.Context) {
	var req struct {
		Name string `json:"name"`
	}
	c.ShouldBindJSON(&req)

	container, err := h.docker.CreateContainer(c.Request.Context(), req.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"id":     container.ID,
		"name":   container.Name,
		"status": container.Status,
		"port":   container.Port,
	})
}

func (h *Handler) deleteContainer(c *gin.Context) {
	id := c.Param("id")
	force := c.Query("force") == "true"

	if err := h.docker.RemoveContainer(c.Request.Context(), id, force); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusNoContent, nil)
}

func (h *Handler) restartContainer(c *gin.Context) {
	id := c.Param("id")
	if err := h.docker.RestartContainer(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Container restarted"})
}

func (h *Handler) getScalingConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"min":      h.config.Scaling.Min,
		"max":      h.config.Scaling.Max,
		"cooldown": h.config.Scaling.Cooldown.String(),
		"triggers": h.config.Scaling.Triggers,
	})
}

func (h *Handler) updateScalingConfig(c *gin.Context) {
	var req struct {
		Min      int                    `json:"min"`
		Max      int                    `json:"max"`
		Cooldown string                 `json:"cooldown"`
		Triggers []config.ScaleTrigger  `json:"triggers"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Min > 0 {
		h.config.Scaling.Min = req.Min
	}
	if req.Max > 0 {
		h.config.Scaling.Max = req.Max
	}

	c.JSON(http.StatusOK, gin.H{"message": "Config updated"})
}

func (h *Handler) manualScale(c *gin.Context) {
	countStr := c.Param("count")
	count, err := strconv.Atoi(countStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid count"})
		return
	}

	event := h.scaler.ManualScale(count)
	c.JSON(http.StatusOK, event)
}

func (h *Handler) getScaleHistory(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "50")
	limit, _ := strconv.Atoi(limitStr)

	events := h.scaler.GetHistory(limit)
	c.JSON(http.StatusOK, gin.H{
		"events": events,
	})
}

func (h *Handler) getHealth(c *gin.Context) {
	containers, _ := h.docker.ListContainers(c.Request.Context())

	running := 0
	healthy := 0
	for _, cont := range containers {
		if cont.Status == "running" {
			running++
			healthy++
		}
	}

	status := "healthy"
	if healthy == 0 {
		status = "unhealthy"
	} else if healthy < len(containers) {
		status = "degraded"
	}

	stats := h.proxy.GetStats()

	c.JSON(http.StatusOK, gin.H{
		"status": status,
		"containers": gin.H{
			"total":     len(containers),
			"running":   running,
			"healthy":   healthy,
			"unhealthy": len(containers) - healthy,
		},
		"proxy": gin.H{
			"status":             "running",
			"active_connections": stats.ActiveConns,
		},
	})
}

func (h *Handler) getContainerHealth(c *gin.Context) {
	containers, _ := h.docker.ListContainers(c.Request.Context())

	var healthy []gin.H
	var unhealthy []gin.H

	for _, cont := range containers {
		info := gin.H{
			"container_id":   cont.ID,
			"container_name": cont.Name,
			"status":         cont.Status,
		}
		if cont.Status == "running" {
			healthy = append(healthy, info)
		} else {
			unhealthy = append(unhealthy, info)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"healthy":   healthy,
		"unhealthy": unhealthy,
	})
}

func (h *Handler) getMetrics(c *gin.Context) {
	window := c.DefaultQuery("window", "current")

	if window == "current" {
		stats := h.proxy.GetStats()
		c.JSON(http.StatusOK, gin.H{
			"timestamp":          stats,
			"active_connections": stats.ActiveConns,
			"total_requests":     stats.TotalRequests,
			"total_429":          stats.Total429,
			"backends":           stats.BackendsHealthy,
		})
		return
	}

	// Get historical metrics
	records, err := h.metrics.GetHistory(window, 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"metrics": records,
	})
}

func (h *Handler) getMetricsHistory(c *gin.Context) {
	window := c.DefaultQuery("window", "1m")
	limitStr := c.DefaultQuery("limit", "100")
	limit, _ := strconv.Atoi(limitStr)

	records, err := h.metrics.GetHistory(window, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"metrics": records,
	})
}

func (h *Handler) getConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"manager": gin.H{
			"api_port":  h.config.Manager.APIPort,
			"log_level": h.config.Manager.LogLevel,
		},
		"proxy": gin.H{
			"listen": h.config.Proxy.Listen,
			"auth": gin.H{
				"enabled": h.config.Proxy.Auth.Enabled,
			},
		},
		"scaling": gin.H{
			"min":      h.config.Scaling.Min,
			"max":      h.config.Scaling.Max,
			"cooldown": h.config.Scaling.Cooldown.String(),
		},
		"loadbalancer": gin.H{
			"algorithm": h.config.LoadBalancer.Algorithm,
		},
		"docker": gin.H{
			"image":   h.config.Docker.Image,
			"network": h.config.Docker.Network,
		},
	})
}

func (h *Handler) updateConfig(c *gin.Context) {
	var req struct {
		Scaling struct {
			Min int `json:"min"`
			Max int `json:"max"`
		} `json:"scaling"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Scaling.Min > 0 {
		h.config.Scaling.Min = req.Scaling.Min
	}
	if req.Scaling.Max > 0 {
		h.config.Scaling.Max = req.Scaling.Max
	}

	c.JSON(http.StatusOK, gin.H{"message": "Config updated"})
}

func (h *Handler) getPrometheusMetrics(c *gin.Context) {
	stats := h.proxy.GetStats()
	containers, _ := h.docker.ListContainers(c.Request.Context())

	var sb strings.Builder

	sb.WriteString("# HELP warp_proxy_total_requests Total proxy requests\n")
	sb.WriteString("# TYPE warp_proxy_total_requests counter\n")
	sb.WriteString(fmt.Sprintf("warp_proxy_total_requests %d\n", stats.TotalRequests))

	sb.WriteString("# HELP warp_proxy_total_429 Total 429 responses\n")
	sb.WriteString("# TYPE warp_proxy_total_429 counter\n")
	sb.WriteString(fmt.Sprintf("warp_proxy_total_429 %d\n", stats.Total429))

	sb.WriteString("# HELP warp_proxy_active_connections Active connections\n")
	sb.WriteString("# TYPE warp_proxy_active_connections gauge\n")
	sb.WriteString(fmt.Sprintf("warp_proxy_active_connections %d\n", stats.ActiveConns))

	sb.WriteString("# HELP warp_proxy_backends_healthy Healthy backends\n")
	sb.WriteString("# TYPE warp_proxy_backends_healthy gauge\n")
	sb.WriteString(fmt.Sprintf("warp_proxy_backends_healthy %d\n", stats.BackendsHealthy))

	running := 0
	for _, cont := range containers {
		if cont.Status == "running" {
			running++
		}
	}

	sb.WriteString("# HELP warp_proxy_containers_running Running containers\n")
	sb.WriteString("# TYPE warp_proxy_containers_running gauge\n")
	sb.WriteString(fmt.Sprintf("warp_proxy_containers_running %d\n", running))

	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(sb.String()))
}
