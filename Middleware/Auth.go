package Middleware

import (
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// BearerAuth middleware checks for valid bearer token in Authorization header or x-api-key header
func BearerAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		expectedToken := os.Getenv("BEARER_TOKEN")
		if expectedToken == "" {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Server configuration error: BEARER_TOKEN not set",
			})
			c.Abort()
			return
		}

		// Check x-api-key header first
		apiKey := c.GetHeader("x-api-key")
		if apiKey != "" {
			if apiKey == expectedToken {
				c.Next()
				return
			}
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid API key",
			})
			c.Abort()
			return
		}

		// Check Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Authorization header or x-api-key header required",
			})
			c.Abort()
			return
		}

		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid authorization format. Expected: Bearer <token>",
			})
			c.Abort()
			return
		}

		token := strings.TrimPrefix(authHeader, "Bearer ")
		token = strings.TrimSpace(token)

		if token != expectedToken {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "Invalid bearer token",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
