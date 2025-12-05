package Models

import "encoding/json"

type AnthropicRequest struct {
	Model       string             `json:"model" binding:"required"`
	Messages    []AnthropicMessage `json:"messages" binding:"required"`
	MaxTokens   int                `json:"max_tokens" binding:"required"`
	Stream      bool               `json:"stream,omitempty"`
	System      AnthropicSystem    `json:"system,omitempty"`
	Tools       []AnthropicTool    `json:"tools,omitempty"`
	Temperature float64            `json:"temperature,omitempty"`
}

type AnthropicSystem struct {
	IsString bool
	String   string
	Blocks   []AnthropicContentBlock
}

func (as *AnthropicSystem) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		as.IsString = true
		as.String = str
		return nil
	}
	var blocks []AnthropicContentBlock
	if err := json.Unmarshal(data, &blocks); err == nil {
		as.IsString = false
		as.Blocks = blocks
		return nil
	}
	return nil
}

func (as AnthropicSystem) GetString() string {
	if as.IsString {
		return as.String
	}
	result := ""
	for _, block := range as.Blocks {
		result += block.Text
	}
	return result
}

type AnthropicMessage struct {
	Role    string                  `json:"role"`
	Content AnthropicMessageContent `json:"content"`
}

type AnthropicMessageContent struct {
	IsString bool
	String   string
	Blocks   []AnthropicContentBlock
}

func (amc *AnthropicMessageContent) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		amc.IsString = true
		amc.String = str
		return nil
	}
	var blocks []AnthropicContentBlock
	if err := json.Unmarshal(data, &blocks); err == nil {
		amc.IsString = false
		amc.Blocks = blocks
		return nil
	}
	return nil
}

func (amc AnthropicMessageContent) MarshalJSON() ([]byte, error) {
	if amc.IsString {
		return json.Marshal(amc.String)
	}
	return json.Marshal(amc.Blocks)
}

type AnthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
	FileID    string `json:"file_id,omitempty"`
}

type AnthropicDocumentSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
	FileID    string `json:"file_id,omitempty"`
}

type AnthropicCitationsConfig struct {
	Enabled bool `json:"enabled,omitempty"`
}

type AnthropicContentBlock struct {
	Type      string      `json:"type"`
	Text      string      `json:"text,omitempty"`
	ToolUseID string      `json:"tool_use_id,omitempty"`
	Content   interface{} `json:"content,omitempty"`
	ID        string      `json:"id,omitempty"`
	Name      string      `json:"name,omitempty"`
	Input     interface{} `json:"input,omitempty"`

	// Image block
	Source *AnthropicImageSource `json:"source,omitempty"`

	// Document block
	Title     string                    `json:"title,omitempty"`
	Context   string                    `json:"context,omitempty"`
	Citations *AnthropicCitationsConfig `json:"citations,omitempty"`

	// Thinking blocks
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"`

	// Tool result blocks
	IsError bool `json:"is_error,omitempty"`

	// Server/MCP tool blocks
	ServerName string `json:"server_name,omitempty"`

	// Web search/fetch result blocks
	URL              string `json:"url,omitempty"`
	RetrievedAt      string `json:"retrieved_at,omitempty"`
	EncryptedContent string `json:"encrypted_content,omitempty"`
	PageAge          string `json:"page_age,omitempty"`
	ErrorCode        string `json:"error_code,omitempty"`

	// Code execution result blocks
	ReturnCode *int   `json:"return_code,omitempty"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
}

func (acb AnthropicContentBlock) GetContentString() string {
	if str, ok := acb.Content.(string); ok {
		return str
	}
	if acb.Content != nil {
		bytes, _ := json.Marshal(acb.Content)
		return string(bytes)
	}
	return ""
}

type AnthropicTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

type AnthropicResponse struct {
	ID         string                  `json:"id"`
	Type       string                  `json:"type"`
	Role       string                  `json:"role"`
	Content    []AnthropicContentBlock `json:"content"`
	Model      string                  `json:"model"`
	StopReason string                  `json:"stop_reason"`
	Usage      AnthropicUsage          `json:"usage"`
}

type AnthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type AnthropicStreamResponse struct {
	Type         string                       `json:"type"`
	Index        *int                         `json:"index,omitempty"`
	Delta        *AnthropicDelta              `json:"delta,omitempty"`
	Message      *AnthropicResponse           `json:"message,omitempty"`
	ContentBlock *AnthropicStreamContentBlock `json:"content_block,omitempty"`
	Usage        *AnthropicUsage              `json:"usage,omitempty"`
}

type AnthropicStreamContentBlock struct {
	Type  string    `json:"type"`
	Text  *string   `json:"text,omitempty"`
	ID    string    `json:"id,omitempty"`
	Name  string    `json:"name,omitempty"`
	Input *struct{} `json:"input,omitempty"`
}

type AnthropicDelta struct {
	Type         string  `json:"type,omitempty"`
	Text         string  `json:"text,omitempty"`
	PartialJson  string  `json:"partial_json,omitempty"`
	StopReason   string  `json:"stop_reason,omitempty"`
	StopSequence *string `json:"stop_sequence"`
}

type AnthropicError struct {
	Type      string               `json:"type"`
	Error     AnthropicErrorDetail `json:"error"`
	RequestID string               `json:"request_id,omitempty"`
}

type AnthropicErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type AnthropicModelsResponse struct {
	Data    []AnthropicModel `json:"data"`
	HasMore bool             `json:"has_more"`
	FirstID string           `json:"first_id"`
	LastID  string           `json:"last_id"`
}

type AnthropicModel struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
}

type AnthropicTokenCountRequest struct {
	Model    string             `json:"model" binding:"required"`
	Messages []AnthropicMessage `json:"messages" binding:"required"`
	System   AnthropicSystem    `json:"system,omitempty"`
	Tools    []AnthropicTool    `json:"tools,omitempty"`
}

type AnthropicTokenCountResponse struct {
	InputTokens int `json:"input_tokens"`
}
