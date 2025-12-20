package Models

import (
	"encoding/json"
)

type ChatCompletionRequest struct {
	Model     string                 `json:"model" binding:"required"`
	Messages  []OpenAiMessage        `json:"messages" binding:"required"`
	Stream    bool                   `json:"stream"`
	Tools     []OpenAiToolDefinition `json:"tools,omitempty"`
	MaxTokens int                    `json:"max_tokens,omitempty"`
	Reasoning string                 `json:"reasoning_effort"`
}

type OpenAiMessage struct {
	Role             string         `json:"role" binding:"required"`
	Content          MessageContent `json:"content"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	ToolCalls        []OpenAiTool   `json:"tool_calls,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
}

// MessageContent can be either a string or []OpenAiContent
type MessageContent struct {
	IsString bool
	String   string
	Contents []OpenAiContent
}

// UnmarshalJSON implements custom unmarshaling for MessageContent
func (mc *MessageContent) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		mc.IsString = true
		mc.String = str
		return nil
	}

	// Try to unmarshal as []OpenAiContent
	var contents []OpenAiContent
	if err := json.Unmarshal(data, &contents); err == nil {
		mc.IsString = false
		mc.Contents = contents
		return nil
	}

	return nil
}

// MarshalJSON implements custom marshaling for MessageContent
func (mc MessageContent) MarshalJSON() ([]byte, error) {
	if mc.IsString {
		return json.Marshal(mc.String)
	}
	return json.Marshal(mc.Contents)
}

// GetString returns the content as a string
func (mc MessageContent) GetString() string {
	if mc.IsString {
		return mc.String
	}
	// Convert OpenAiContent array to string (concatenate text fields)
	var result string
	for _, content := range mc.Contents {
		result += content.Text + "\n"
	}
	return result
}

func (mc MessageContent) GetBytes() []byte {
	if mc.IsString {
		return []byte(mc.String)
	}
	// Convert OpenAiContent array to bytes (concatenate text fields)
	var result []byte
	for _, content := range mc.Contents {
		result = append(result, []byte(content.Text)...)
		result = append(result, '\n')
	}
	return result
}

type OpenAiContent struct {
	Type         string         `json:"type"`
	Text         string         `json:"text,omitempty"`
	ImageUrl     OpenAiImageUrl `json:"image_url,omitempty"`
	CacheControl interface{}    `json:"cache_control,omitempty"`
}

type OpenAiImageUrl struct {
	Url string `json:"url"`
}

type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int           `json:"index"`
	Message      OpenAiMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// SSEResponse represents a single chunk in the streaming response
// Reference: https://platform.openai.com/docs/api-reference/chat-streaming/streaming
type SSEResponse struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"` // Should be "chat.completion.chunk"
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Choices []SSEChoice `json:"choices"`
}

// SSEChoice represents a choice in the streaming response
type SSEChoice struct {
	Index        int     `json:"index"`
	Delta        Delta   `json:"delta"`
	FinishReason *string `json:"finish_reason"` // null until the last chunk
}

// Delta contains the incremental content for streaming responses
type Delta struct {
	Role      string          `json:"role,omitempty"`    // Only present in the first chunk
	Content   string          `json:"content,omitempty"` // Incremental content
	Reasoning string          `json:"reasoning_content,omitempty"`
	ToolCalls []SSEOpenAiTool `json:"tool_calls,omitempty"`
}

type OpenAiTool struct {
	Index    int            `json:"index"`
	Function OpenAIFunction `json:"function"`
	Id       string         `json:"id"`
	Type     string         `json:"type"`
}

type SSEOpenAiTool struct {
	Index    int               `json:"index"`
	Function SSEOpenAIFunction `json:"function"`
	Id       string            `json:"id"`
	Type     string            `json:"type"`
}

type OpenAIFunction struct {
	Arguments json.RawMessage `json:"arguments"`
	Name      string          `json:"name"`
}

type SSEOpenAIFunction struct {
	Arguments string `json:"arguments,omitempty"`
	Name      string `json:"name,omitempty"`
}

// OpenAiToolDefinition represents a tool definition in the request
type OpenAiToolDefinition struct {
	Type     string                   `json:"type"` // "function"
	Function OpenAIFunctionDefinition `json:"function"`
}

// OpenAIFunctionDefinition represents a function definition
type OpenAIFunctionDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type OpenAIModelsResponse struct {
	Object string        `json:"object"`
	Data   []OpenAIModel `json:"data"`
}

type OpenAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}
