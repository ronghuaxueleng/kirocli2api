package Utils

import (
	"fmt"
	"kilocli2api/Models"
)

func ValidateChatCompletionRequest(req *Models.ChatCompletionRequest) error {
	if req.Model == "" {
		return fmt.Errorf("model is required")
	}
	if len(req.Model) < 1 || len(req.Model) > 256 {
		return fmt.Errorf("model must be between 1 and 256 characters")
	}
	if len(req.Messages) == 0 {
		return fmt.Errorf("messages array cannot be empty")
	}
	for i, msg := range req.Messages {
		if msg.Role == "" {
			return fmt.Errorf("message[%d]: role is required", i)
		}
		if msg.Role != "system" && msg.Role != "user" && msg.Role != "assistant" && msg.Role != "tool" {
			return fmt.Errorf("message[%d]: invalid role '%s'", i, msg.Role)
		}
	}
	if req.MaxTokens < 0 {
		return fmt.Errorf("max_tokens must be non-negative")
	}
	return nil
}

func ValidateAnthropicRequest(req *Models.AnthropicRequest) error {
	if req.Model == "" {
		return fmt.Errorf("model is required")
	}
	if len(req.Model) < 1 || len(req.Model) > 256 {
		return fmt.Errorf("model must be between 1 and 256 characters")
	}
	if len(req.Messages) == 0 {
		return fmt.Errorf("messages array is required and cannot be empty")
	}
	if req.MaxTokens < 1 {
		return fmt.Errorf("max_tokens is required and must be at least 1")
	}
	for i, msg := range req.Messages {
		if msg.Role == "" {
			return fmt.Errorf("messages[%d]: role is required", i)
		}
		if msg.Role != "user" && msg.Role != "assistant" {
			return fmt.Errorf("messages[%d]: role must be 'user' or 'assistant'", i)
		}
	}
	if req.Temperature < 0 || req.Temperature > 1 {
		return fmt.Errorf("temperature must be between 0 and 1")
	}
	return nil
}
