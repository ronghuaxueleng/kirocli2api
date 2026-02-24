package Models

import "encoding/json"

// QAPIRequest is the top-level structure for the entire JSON body.
type QAPIRequest struct {
	ConversationState QConversationState `json:"conversationState"`
}

// QConversationState holds the complete state of the chat conversation.
type QConversationState struct {
	ConversationID  string          `json:"conversationId"`
	History         []QHistoryItem  `json:"history,omitempty"`
	CurrentMessage  QCurrentMessage `json:"currentMessage"`
	ChatTriggerType string          `json:"chatTriggerType"`
	AgentTaskType   string          `json:"agentTaskType,omitempty"`
}

// QHistoryItem represents a single entry in the conversation history,
// which can be either a user message or an assistant response.
type QHistoryItem struct {
	UserInputMessage         *QUserInputMessageHistory  `json:"userInputMessage,omitempty"`
	AssistantResponseMessage *QAssistantResponseMessage `json:"assistantResponseMessage,omitempty"`
}

type QImage struct {
	Format string       `json:"format,omitempty"`
	Source QImageSource `json:"source,omitempty"`
}

type QImageSource struct {
	Bytes string `json:"bytes,omitempty"`
}

// QUserInputMessage contains the details of a user's message.
type QUserInputMessage struct {
	Content                 string                   `json:"content"`
	UserInputMessageContext QUserInputMessageContext `json:"userInputMessageContext"`
	Origin                  string                   `json:"origin"`
	QImage                  []QImage                 `json:"images,omitempty"`
	ModelID                 string                   `json:"modelId"`
}

// QUserInputMessageHistory contains the details of a user's message. It's for history
type QUserInputMessageHistory struct {
	Content                 string                   `json:"content"`
	UserInputMessageContext QUserInputMessageContext `json:"userInputMessageContext"`
	Origin                  string                   `json:"origin"`
	QImage                  []QImage                 `json:"images,omitempty"`
}

// QAssistantResponseMessage contains the details of the assistant's response.
type QAssistantResponseMessage struct {
	MessageID string      `json:"messageId"`
	Content   string      `json:"content"`
	ToolUses  *[]QToolUse `json:"toolUses,omitempty"`
}

// QUserInputMessageContext provides the context for a user's message,
// including environment state and results from previous tool calls.
type QUserInputMessageContext struct {
	EnvState    QEnvState          `json:"envState"`
	ToolResults *[]QToolResultItem `json:"toolResults,omitempty"`
	Tools       *[]QTool           `json:"tools,omitempty"`
}

// QEnvState describes the user's local environment.
type QEnvState struct {
	OperatingSystem         string `json:"operatingSystem"`
	CurrentWorkingDirectory string `json:"currentWorkingDirectory"`
}

// QToolResultItem holds the output from a single tool execution.
type QToolResultItem struct {
	ToolUseID string               `json:"toolUseId,omitempty"`
	Content   []QToolResultContent `json:"content,omitempty"`
	Status    string               `json:"status,omitempty"`
}

// QToolResultContent holds either text or JSON output from a tool.
type QToolResultContent struct {
	Text string           `json:"text,omitempty"`
	JSON *json.RawMessage `json:"json,omitempty"`
}

// QToolUse represents a tool call made by the assistant.
// The Input field is a json.RawMessage to handle JSON objects.
type QToolUse struct {
	ToolUseID string      `json:"toolUseId"`
	Name      string      `json:"name"`
	Input     interface{} `json:"input,omitempty"`
}

// QCurrentMessage represents the most recent message in the conversation flow.
type QCurrentMessage struct {
	UserInputMessage QUserInputMessage `json:"userInputMessage"`
}

// QTool defines an available tool and its specification.
type QTool struct {
	ToolSpecification QToolSpecification `json:"toolSpecification"`
}

// QToolSpecification contains the details and schema for a tool.
type QToolSpecification struct {
	InputSchema QInputSchema `json:"inputSchema"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
}

// QInputSchema defines the structure of the input for a tool.
type QInputSchema struct {
	JSON map[string]interface{} `json:"json"`
}

type QSSEResponse struct {
	// AssistantResponseEvent,CodeEvent
	Content string `json:"content,omitempty"`

	// InvalidStateEvent
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`

	// MessageMetadataEvent
	ConversationId string `json:"conversation_id,omitempty"`
	UtteranceId    string `json:"utterance_id,omitempty"`

	// ToolUseEvent
	ToolUseId string `json:"toolUseId,omitempty"`
	Name      string `json:"name,omitempty"`
	Input     string `json:"input,omitempty"`
	Stop      bool   `json:"stop,omitempty"`
}

type QSSEToolCallCache struct {
	ToolUseId string `json:"toolUseId,omitempty"`
	Name      string `json:"name,omitempty"`
	Input     string `json:"input,omitempty"`
	Stop      bool   `json:"stop,omitempty"`
	ToolIndex int
}

type QModelsResponse struct {
	DefaultModel QModel   `json:"defaultModel"`
	Models       []QModel `json:"models"`
}

type QModel struct {
	ModelID   string `json:"modelId"`
	ModelName string `json:"modelName"`
}
