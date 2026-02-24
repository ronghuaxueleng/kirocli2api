package API

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"kilocli2api/Models"
	"kilocli2api/Utils"
	"net/http"
)

func DebugAnthropic2Q(c *gin.Context) {
	var req Models.AnthropicRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	result, err := Utils.MapAnthropicToAmazonQ(req, uuid.NewString(), ".")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}
