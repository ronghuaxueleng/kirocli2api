package API

import "github.com/gin-gonic/gin"

func NotFound(c *gin.Context) {
	c.JSON(404, gin.H{"error": "Endpoint not found"})
}
