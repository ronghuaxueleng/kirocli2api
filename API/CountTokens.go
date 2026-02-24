package API

import (
	"github.com/gin-gonic/gin"
	"kilocli2api/Models"
	"net/http"
)

func CountTokens(c *gin.Context) {
	var req Models.AnthropicTokenCountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		anthropicError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	inputTokens := estimateInputTokens(Models.AnthropicRequest{
		Model:    req.Model,
		Messages: req.Messages,
		System:   req.System,
		Tools:    req.Tools,
	})

	c.JSON(http.StatusOK, Models.AnthropicTokenCountResponse{
		InputTokens: inputTokens,
	})
}
