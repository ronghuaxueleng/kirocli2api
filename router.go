package main

import (
	"kilocli2api/API"
	"kilocli2api/Middleware"

	"github.com/gin-gonic/gin"
)

func setupRouter(r *gin.Engine) {
	v1 := r.Group("/v1")
	v1.Use(Middleware.BearerAuth()) // Apply bearer token authentication
	{
		v1.POST("/chat/completions", API.ChatCompletions)
		v1.POST("/messages", API.Messages)
		v1.POST("/messages/count_tokens", API.CountTokens)
		v1.GET("/models", API.ListModels)
	}

	// Debug endpoint without authentication
	r.POST("/debug/token", API.DebugToken)
	r.POST("/debug/anthropic2q", API.DebugAnthropic2Q)

	r.NoRoute(API.NotFound)
}
