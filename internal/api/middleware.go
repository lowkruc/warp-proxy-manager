package api

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lowkruc/warp-proxy-manager/internal/config"
)

func AuthMiddleware(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !cfg.Proxy.Auth.Enabled {
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "Authorization header required",
			})
			return
		}

		// Check Bearer token
		if strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			// For now, accept any non-empty token
			// In production, validate JWT
			if token == "" {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": "Invalid token",
				})
				return
			}
			c.Next()
			return
		}

		// Check Basic auth
		if strings.HasPrefix(authHeader, "Basic ") {
			decoded := strings.TrimPrefix(authHeader, "Basic ")
			// Decode base64
			parts := strings.SplitN(decoded, ":", 2)
			if len(parts) != 2 {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": "Invalid Basic auth format",
				})
				return
			}

			username := parts[0]
			password := parts[1]

			// Check against users
			valid := false
			for _, user := range cfg.Proxy.Auth.Users {
				if subtle.ConstantTimeCompare([]byte(username), []byte(user.User)) == 1 &&
					subtle.ConstantTimeCompare([]byte(password), []byte(user.Pass)) == 1 {
					valid = true
					break
				}
			}

			if !valid {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error": "Invalid credentials",
				})
				return
			}

			c.Next()
			return
		}

		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
			"error": "Invalid authorization scheme",
		})
	}
}

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func LoggerMiddleware() gin.HandlerFunc {
	return gin.Logger()
}
