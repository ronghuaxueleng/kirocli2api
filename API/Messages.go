package API

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"kilocli2api/Models"
	"kilocli2api/Utils"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tiktoken-go/tokenizer"
)

var cl100kEncoder tokenizer.Codec

func init() {
	cl100kEncoder, _ = tokenizer.Get(tokenizer.Cl100kBase)
}

func countTokens(text string) int {
	if text == "" {
		return 0
	}
	text = strings.ReplaceAll(text, "<thinking>", "")
	text = strings.ReplaceAll(text, "</thinking>", "")
	if cl100kEncoder == nil {
		return len(text) / 4
	}
	tokens, _, _ := cl100kEncoder.Encode(text)
	return len(tokens)
}

func estimateInputTokens(req Models.AnthropicRequest) int {
	var parts []string
	parts = append(parts, req.System.GetString())
	for _, msg := range req.Messages {
		if msg.Content.IsString {
			parts = append(parts, msg.Content.String)
		} else {
			for _, block := range msg.Content.Blocks {
				parts = append(parts, block.Text)
				if block.Type == "tool_use" {
					parts = append(parts, block.Name)
					if input, err := json.Marshal(block.Input); err == nil {
						parts = append(parts, string(input))
					}
				} else if block.Type == "tool_result" {
					parts = append(parts, block.GetContentString())
				}
			}
		}
	}
	for _, tool := range req.Tools {
		parts = append(parts, tool.Name, tool.Description)
		if schema, err := json.Marshal(tool.InputSchema); err == nil {
			parts = append(parts, string(schema))
		}
	}
	return countTokens(strings.Join(parts, "\n"))
}

func anthropicError(c *gin.Context, status int, errorType, message string) {
	requestID := "req_" + uuid.NewString()
	c.Header("request-id", requestID)
	c.JSON(status, Models.AnthropicError{
		Type:      "error",
		Error:     Models.AnthropicErrorDetail{Type: errorType, Message: message},
		RequestID: requestID,
	})
}

func hasWebSearchTool(req Models.AnthropicRequest) bool {
	for _, tool := range req.Tools {
		if tool.Name == "web_search" {
			return true
		}
	}
	return false
}

func Messages(c *gin.Context) {
	bodyBytes, _ := io.ReadAll(c.Request.Body)
	c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	var req Models.AnthropicRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Utils.ErrorLogger.Printf("400 Error - JSON Binding Failed\nEndpoint: %s\nRequest Body: %s\nError: %v", c.Request.URL, string(bodyBytes), err)
		anthropicError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	if err := Utils.ValidateAnthropicRequest(&req); err != nil {
		Utils.ErrorLogger.Printf("400 Error - Validation Failed\nEndpoint: %s\nRequest Body: %s\nError: %v", c.Request.URL, string(bodyBytes), err)
		anthropicError(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	// Debug: Log all tool names
	if len(req.Tools) > 0 {
		toolNames := make([]string, len(req.Tools))
		for i, tool := range req.Tools {
			toolNames[i] = tool.Name
		}
		Utils.NormalLogger.Printf("Request has %d tools: %v", len(req.Tools), toolNames)
	}

	// Route to MCP if websearch detected
	if hasWebSearchTool(req) {
		Utils.NormalLogger.Printf("WebSearch tool detected, routing to MCP endpoint")
		if req.Stream {
			handleMCPStreamingRequest(c, req)
		} else {
			handleMCPNonStreamingRequest(c, req)
		}
		return
	}

	// Normal Q API flow
	if req.Stream {
		handleAnthropicStreamingRequest(c, req)
	} else {
		handleAnthropicNonStreamingRequest(c, req)
	}
}

func handleAnthropicNonStreamingRequest(c *gin.Context, req Models.AnthropicRequest) {
	incomingBytes, _ := json.Marshal(req)
	QRequest, err := Utils.MapAnthropicToAmazonQ(req, uuid.New().String(), ".")
	if err != nil {
		anthropicError(c, http.StatusInternalServerError, "api_error", "Failed to map request")
		Utils.LogRequestError(string(incomingBytes), QRequest)
		panic(err)
		return
	}

	jsonBytes, _ := json.Marshal(QRequest)
	qUrl := os.Getenv("AMAZON_Q_URL")
	client := &http.Client{
		Transport: Utils.GetProxyTransport(),
		Timeout:   5 * time.Minute,
	}

	for attempt := 0; attempt < 3; attempt++ {
		httpReq, _ := http.NewRequest("POST", qUrl, bytes.NewReader(jsonBytes))
		httpReq.Header.Set("user-agent", "aws-sdk-rust/1.3.10 ua/2.1 api/codewhispererstreaming/0.1.12842 os/macos lang/rust/1.88.0 md/appVersion-1.23.1 app/AmazonQ-For-CLI")
		httpReq.Header.Set("accept", "*/*")
		httpReq.Header.Set("accept-encoding", "gzip")
		httpReq.Header.Set("content-type", "application/x-amz-json-1.0")
		httpReq.Header.Set("x-amz-target", "AmazonCodeWhispererStreamingService.GenerateAssistantResponse")
		httpReq.Header.Set("x-amz-user-agent", "aws-sdk-rust/1.3.10 ua/2.1 api/codewhispererstreaming/0.1.12842 os/macos lang/rust/1.88.0 m/F app/AmazonQ-For-CLI")
		httpReq.Header.Set("x-amzn-codewhisperer-optout", "true")
		httpReq.Header.Set("amz-sdk-request", "attempt=1; max=3")
		httpReq.Header.Set("amz-sdk-invocation-id", uuid.NewString())

		bearer, err := Utils.GetBearer()
		if err != nil {
			anthropicError(c, http.StatusInternalServerError, "api_error", "Failed to get bearer token")
			Utils.NormalLogger.Println("Error getting bearer token:", err)
			return
		}
		httpReq.Header.Set("Authorization", "Bearer "+bearer)

		resp, err := client.Do(httpReq)
		if err != nil {
			anthropicError(c, http.StatusInternalServerError, "api_error", "Failed to get response")
			Utils.ErrorLogger.Printf("Error getting response from Q API: %v\nRequest body: %s", err, string(jsonBytes))
			return
		}

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusBadRequest {
				Utils.ErrorLogger.Printf("400 Error - Endpoint: %s\nUser Request: %s\nMapped Request: %s\nAmazon Response: %s", c.Request.URL, string(incomingBytes), string(jsonBytes), string(bodyBytes))
			}
			Utils.CheckAndDisableToken(bodyBytes, bearer)
			if attempt < 2 {
				continue
			}
			errorType := "api_error"
			if resp.StatusCode == http.StatusBadRequest {
				errorType = "invalid_request_error"
			}
			anthropicError(c, http.StatusBadRequest, errorType, string(bodyBytes))
			return
		}

		decoder := eventstream.NewDecoder()
		payloadBuf := make([]byte, 1024*1024)

		fullContent, toolCalls, err := Utils.ProcessQStreamToOpenAI(decoder, resp.Body, payloadBuf)
		resp.Body.Close()
		if err != nil {
			anthropicError(c, http.StatusInternalServerError, "api_error", err.Error())
			Utils.NormalLogger.Println("Error:", err)
			return
		}

		inputTokens := estimateInputTokens(req)
		var outputText strings.Builder
		outputText.WriteString(fullContent)
		for _, tc := range toolCalls {
			outputText.WriteString(tc.Function.Name)
			outputText.WriteString(string(tc.Function.Arguments))
		}
		outputTokens := countTokens(outputText.String())

		response := Models.AnthropicResponse{
			ID:         "msg-" + uuid.NewString(),
			Type:       "message",
			Role:       "assistant",
			Content:    openAIToAnthropicContent(fullContent, toolCalls),
			Model:      req.Model,
			StopReason: stopReason(toolCalls),
			Usage:      Models.AnthropicUsage{InputTokens: inputTokens, OutputTokens: outputTokens},
		}

		c.JSON(http.StatusOK, response)
		return
	}
}

func handleAnthropicStreamingRequest(c *gin.Context, req Models.AnthropicRequest) {
	incomingBytes, _ := json.Marshal(req)
	QRequest, err := Utils.MapAnthropicToAmazonQ(req, uuid.New().String(), ".")
	if err != nil {
		anthropicError(c, http.StatusInternalServerError, "api_error", "Failed to map request")
		Utils.LogRequestError(string(incomingBytes), QRequest)
		return
	}

	jsonBytes, _ := json.Marshal(QRequest)
	qUrl := os.Getenv("AMAZON_Q_URL")
	client := &http.Client{
		Transport: Utils.GetProxyTransport(),
		Timeout:   5 * time.Minute,
	}

	var resp *http.Response
	var bearer string
	for attempt := 0; attempt < 3; attempt++ {
		httpReq, _ := http.NewRequest("POST", qUrl, bytes.NewReader(jsonBytes))
		httpReq.Header.Set("user-agent", "aws-sdk-rust/1.3.10 ua/2.1 api/codewhispererstreaming/0.1.12842 os/macos lang/rust/1.88.0 md/appVersion-1.23.1 app/AmazonQ-For-CLI")
		httpReq.Header.Set("accept", "*/*")
		httpReq.Header.Set("accept-encoding", "gzip")
		httpReq.Header.Set("content-type", "application/x-amz-json-1.0")
		httpReq.Header.Set("x-amz-target", "AmazonCodeWhispererStreamingService.GenerateAssistantResponse")
		httpReq.Header.Set("x-amz-user-agent", "aws-sdk-rust/1.3.10 ua/2.1 api/codewhispererstreaming/0.1.12842 os/macos lang/rust/1.88.0 m/F app/AmazonQ-For-CLI")
		httpReq.Header.Set("x-amzn-codewhisperer-optout", "true")
		httpReq.Header.Set("amz-sdk-request", "attempt=1; max=3")
		httpReq.Header.Set("amz-sdk-invocation-id", uuid.NewString())

		bearer, err = Utils.GetBearer()
		if err != nil {
			anthropicError(c, http.StatusInternalServerError, "api_error", "Failed to get bearer token")
			Utils.NormalLogger.Println("Error getting bearer token:", err)
			return
		}
		httpReq.Header.Set("Authorization", "Bearer "+bearer)

		resp, err = client.Do(httpReq)
		if err != nil {
			anthropicError(c, http.StatusInternalServerError, "api_error", "Failed to get response")
			Utils.ErrorLogger.Printf("Error getting response from Q API: %v\nRequest body: %s", err, string(jsonBytes))
			return
		}

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusBadRequest {
				Utils.ErrorLogger.Printf("400 Error - Endpoint: %s\nUser Request: %s\nMapped Request: %s\nAmazon Response: %s", c.Request.URL, string(incomingBytes), string(jsonBytes), string(bodyBytes))
			}
			Utils.CheckAndDisableToken(bodyBytes, bearer)
			if attempt < 2 {
				continue
			}
			errorType := "api_error"
			if resp.StatusCode == http.StatusBadRequest {
				errorType = "invalid_request_error"
			}
			anthropicError(c, http.StatusBadRequest, errorType, string(bodyBytes))
			return
		}
		break
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			Utils.NormalLogger.Printf("Close network io reader fail: %s\n", err)
		}
	}(resp.Body)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	decoder := eventstream.NewDecoder()
	payloadBuf := make([]byte, 1024*1024)
	messageCount := 0
	blockIndex := -1
	activeTextIndex := -1
	activeThinkingIndex := -1
	toolBlockIndices := make(map[string]int)
	toolOrder := make([]string, 0)
	hasToolUse := false
	inputTokens := estimateInputTokens(req)
	var textBuffer strings.Builder
	var thinkingBuffer strings.Builder
	toolInputBuffers := make(map[string]*strings.Builder)
	inThinking := false

	indexPtr := func(v int) *int {
		i := v
		return &i
	}

	closeTextBlock := func() {
		if activeTextIndex >= 0 {
			c.SSEvent("content_block_stop", Models.AnthropicStreamResponse{Type: "content_block_stop", Index: indexPtr(activeTextIndex)})
			activeTextIndex = -1
		}
	}

	closeThinkingBlock := func() {
		if activeThinkingIndex >= 0 {
			c.SSEvent("content_block_stop", Models.AnthropicStreamResponse{Type: "content_block_stop", Index: indexPtr(activeThinkingIndex)})
			activeThinkingIndex = -1
			inThinking = false
		}
	}

	closeToolBlock := func(toolID string) {
		if idx, ok := toolBlockIndices[toolID]; ok {
			c.SSEvent("content_block_stop", Models.AnthropicStreamResponse{Type: "content_block_stop", Index: indexPtr(idx)})
			delete(toolBlockIndices, toolID)
		}
	}

	pingTicker := time.NewTicker(5 * time.Second)
	defer pingTicker.Stop()

	for {
		if c.Writer.Written() && c.IsAborted() {
			return
		}

		select {
		case <-pingTicker.C:
			c.SSEvent("ping", Models.AnthropicStreamResponse{Type: "ping"})
			c.Writer.Flush()
			continue
		default:
		}

		msg, err := decoder.Decode(resp.Body, payloadBuf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return
		}

		if messageCount == 0 {
			c.SSEvent("message_start", Models.AnthropicStreamResponse{
				Type: "message_start",
				Message: &Models.AnthropicResponse{
					ID:         "msg-" + uuid.NewString(),
					Type:       "message",
					Role:       "assistant",
					Content:    []Models.AnthropicContentBlock{},
					Model:      req.Model,
					StopReason: "",
					Usage:      Models.AnthropicUsage{InputTokens: inputTokens, OutputTokens: 1},
				},
			})
			c.SSEvent("ping", Models.AnthropicStreamResponse{Type: "ping"})
		}

		var qMsg Models.QSSEResponse
		if err := json.Unmarshal(msg.Payload, &qMsg); err != nil {
			anthropicError(c, http.StatusInternalServerError, "api_error", err.Error())
			Utils.NormalLogger.Printf("Json unmarshal error: %s\n", err)
			return
		}

		switch {
		case qMsg.Content != "":
			content := qMsg.Content
			for len(content) > 0 {
				if !inThinking {
					if idx := strings.Index(content, "<thinking>"); idx >= 0 {
						if idx > 0 {
							if activeTextIndex == -1 {
								blockIndex++
								activeTextIndex = blockIndex
								emptyText := ""
								c.SSEvent("content_block_start", Models.AnthropicStreamResponse{
									Type:  "content_block_start",
									Index: indexPtr(activeTextIndex),
									ContentBlock: &Models.AnthropicStreamContentBlock{
										Type: "text",
										Text: &emptyText,
									},
								})
							}
							textBuffer.WriteString(content[:idx])
							c.SSEvent("content_block_delta", Models.AnthropicStreamResponse{
								Type:  "content_block_delta",
								Index: indexPtr(activeTextIndex),
								Delta: &Models.AnthropicDelta{
									Type: "text_delta",
									Text: content[:idx],
								},
							})
						} else {
							closeTextBlock()
						}
						blockIndex++
						activeThinkingIndex = blockIndex
						inThinking = true
						emptyThinking := ""
						c.SSEvent("content_block_start", Models.AnthropicStreamResponse{
							Type:  "content_block_start",
							Index: indexPtr(activeThinkingIndex),
							ContentBlock: &Models.AnthropicStreamContentBlock{
								Type:     "thinking",
								Thinking: &emptyThinking,
							},
						})
						content = content[idx+10:]
					} else {
						if activeTextIndex == -1 {
							blockIndex++
							activeTextIndex = blockIndex
							emptyText := ""
							c.SSEvent("content_block_start", Models.AnthropicStreamResponse{
								Type:  "content_block_start",
								Index: indexPtr(activeTextIndex),
								ContentBlock: &Models.AnthropicStreamContentBlock{
									Type: "text",
									Text: &emptyText,
								},
							})
						}
						textBuffer.WriteString(content)
						c.SSEvent("content_block_delta", Models.AnthropicStreamResponse{
							Type:  "content_block_delta",
							Index: indexPtr(activeTextIndex),
							Delta: &Models.AnthropicDelta{
								Type: "text_delta",
								Text: content,
							},
						})
						break
					}
				} else {
					if idx := strings.Index(content, "</thinking>"); idx >= 0 {
						if idx > 0 {
							thinkingBuffer.WriteString(content[:idx])
							c.SSEvent("content_block_delta", Models.AnthropicStreamResponse{
								Type:  "content_block_delta",
								Index: indexPtr(activeThinkingIndex),
								Delta: &Models.AnthropicDelta{
									Type:     "thinking_delta",
									Thinking: content[:idx],
								},
							})
						}
						closeThinkingBlock()
						content = content[idx+11:]
					} else {
						thinkingBuffer.WriteString(content)
						c.SSEvent("content_block_delta", Models.AnthropicStreamResponse{
							Type:  "content_block_delta",
							Index: indexPtr(activeThinkingIndex),
							Delta: &Models.AnthropicDelta{
								Type:     "thinking_delta",
								Thinking: content,
							},
						})
						break
					}
				}
			}
		case qMsg.ToolUseId != "":
			hasToolUse = true
			idx, exists := toolBlockIndices[qMsg.ToolUseId]
			if !exists {
				closeTextBlock()
				closeThinkingBlock()
				blockIndex++
				idx = blockIndex
				toolBlockIndices[qMsg.ToolUseId] = idx
				toolOrder = append(toolOrder, qMsg.ToolUseId)
				toolInputBuffers[qMsg.ToolUseId] = &strings.Builder{}
				name := qMsg.Name
				if name == "" {
					name = qMsg.ToolUseId
				}
				c.SSEvent("content_block_start", Models.AnthropicStreamResponse{
					Type:  "content_block_start",
					Index: indexPtr(idx),
					ContentBlock: &Models.AnthropicStreamContentBlock{
						Type:  "tool_use",
						ID:    qMsg.ToolUseId,
						Name:  name,
						Input: &struct{}{},
					},
				})
			} else if qMsg.Name != "" {
				// If Amazon Q re-sends the name mid-stream, nothing to do, but keep branch for clarity
				_ = idx
			}

			if qMsg.Input != "" {
				if buf, ok := toolInputBuffers[qMsg.ToolUseId]; ok {
					buf.WriteString(qMsg.Input)
				}
				c.SSEvent("content_block_delta", Models.AnthropicStreamResponse{
					Type:  "content_block_delta",
					Index: indexPtr(idx),
					Delta: &Models.AnthropicDelta{
						Type:        "input_json_delta",
						PartialJson: qMsg.Input,
					},
				})
			}

			if qMsg.Stop {
				closeToolBlock(qMsg.ToolUseId)
			}
		case qMsg.Reason != "":
			closeTextBlock()
			closeThinkingBlock()
			for _, toolID := range toolOrder {
				closeToolBlock(toolID)
			}
			var allToolInputs strings.Builder
			for _, toolID := range toolOrder {
				if buf, ok := toolInputBuffers[toolID]; ok {
					allToolInputs.WriteString(buf.String())
				}
			}
			outputTokens := countTokens(textBuffer.String() + thinkingBuffer.String() + allToolInputs.String())
			c.SSEvent("message_delta", Models.AnthropicStreamResponse{
				Type:  "message_delta",
				Delta: &Models.AnthropicDelta{StopReason: "error", StopSequence: nil},
				Usage: &Models.AnthropicUsage{OutputTokens: outputTokens},
			})
			c.SSEvent("message_stop", Models.AnthropicStreamResponse{Type: "message_stop"})
			return
		}

		messageCount++
	}

	closeTextBlock()
	closeThinkingBlock()
	for _, toolID := range toolOrder {
		closeToolBlock(toolID)
	}

	stopReason := "end_turn"
	if hasToolUse {
		stopReason = "tool_use"
	}

	var allToolInputs strings.Builder
	for _, toolID := range toolOrder {
		if buf, ok := toolInputBuffers[toolID]; ok {
			allToolInputs.WriteString(buf.String())
		}
	}
	outputTokens := countTokens(textBuffer.String() + thinkingBuffer.String() + allToolInputs.String())

	c.SSEvent("message_delta", Models.AnthropicStreamResponse{
		Type:  "message_delta",
		Delta: &Models.AnthropicDelta{StopReason: stopReason, StopSequence: nil},
		Usage: &Models.AnthropicUsage{OutputTokens: outputTokens},
	})
	c.SSEvent("message_stop", Models.AnthropicStreamResponse{Type: "message_stop"})
}

func openAIToAnthropicContent(content string, toolCalls []Models.OpenAiTool) []Models.AnthropicContentBlock {
	blocks := make([]Models.AnthropicContentBlock, 0, len(toolCalls)+2)

	for len(content) > 0 {
		if thinkingStart := strings.Index(content, "<thinking>"); thinkingStart >= 0 {
			if thinkingStart > 0 && strings.TrimSpace(content[:thinkingStart]) != "" {
				blocks = append(blocks, Models.AnthropicContentBlock{Type: "text", Text: content[:thinkingStart]})
			}
			content = content[thinkingStart+10:]
			if thinkingEnd := strings.Index(content, "</thinking>"); thinkingEnd >= 0 {
				blocks = append(blocks, Models.AnthropicContentBlock{Type: "thinking", Thinking: content[:thinkingEnd]})
				content = content[thinkingEnd+11:]
			} else {
				blocks = append(blocks, Models.AnthropicContentBlock{Type: "thinking", Thinking: content})
				break
			}
		} else {
			if strings.TrimSpace(content) != "" {
				blocks = append(blocks, Models.AnthropicContentBlock{Type: "text", Text: content})
			}
			break
		}
	}

	for _, tc := range toolCalls {
		blocks = append(blocks, Models.AnthropicContentBlock{
			Type:  "tool_use",
			ID:    tc.Id,
			Name:  tc.Function.Name,
			Input: anthropicToolInput(tc.Function.Arguments),
		})
	}
	return blocks
}

func stopReason(toolCalls []Models.OpenAiTool) string {
	if len(toolCalls) > 0 {
		return "tool_use"
	}
	return "end_turn"
}

func anthropicToolInput(raw json.RawMessage) interface{} {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return map[string]interface{}{}
	}

	var decoded interface{}
	if err := json.Unmarshal(raw, &decoded); err == nil {
		if decoded == nil {
			return map[string]interface{}{}
		}
		return decoded
	} else {
		Utils.NormalLogger.Printf("Json unmarshal error while building anthropic tool input: %v\n", err)
	}

	return trimmed
}

// MCP JSON-RPC structures
type MCPRequest struct {
	ID      string    `json:"id"`
	JSONRPC string    `json:"jsonrpc"`
	Method  string    `json:"method"`
	Params  MCPParams `json:"params"`
}

type MCPParams struct {
	Name      string       `json:"name"`
	Arguments MCPArguments `json:"arguments"`
}

type MCPArguments struct {
	Query string `json:"query"`
}

type MCPResponse struct {
	ID      string     `json:"id"`
	JSONRPC string     `json:"jsonrpc"`
	Result  *MCPResult `json:"result,omitempty"`
	Error   *MCPError  `json:"error,omitempty"`
}

type MCPResult struct {
	Content []MCPContent `json:"content"`
	IsError bool         `json:"isError"`
}

type MCPContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type WebSearchResults struct {
	Results      []WebSearchResult `json:"results"`
	TotalResults int               `json:"totalResults,omitempty"`
	Query        string            `json:"query,omitempty"`
}

type WebSearchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Snippet     string `json:"snippet,omitempty"`
	Domain      string `json:"domain,omitempty"`
	PublishedAt int64  `json:"published_date,omitempty"`
}

func extractSearchQuery(req Models.AnthropicRequest) string {
	if len(req.Messages) == 0 {
		return ""
	}
	firstMsg := req.Messages[0]
	if firstMsg.Content.IsString {
		text := firstMsg.Content.String
		prefix := "Perform a web search for the query: "
		if strings.HasPrefix(text, prefix) {
			return strings.TrimSpace(text[len(prefix):])
		}
		return text
	}
	if len(firstMsg.Content.Blocks) > 0 {
		text := firstMsg.Content.Blocks[0].Text
		prefix := "Perform a web search for the query: "
		if strings.HasPrefix(text, prefix) {
			return strings.TrimSpace(text[len(prefix):])
		}
		return text
	}
	return ""
}

func handleMCPNonStreamingRequest(c *gin.Context, req Models.AnthropicRequest) {
	handleMCPStreamingRequest(c, req)
}

func handleMCPStreamingRequest(c *gin.Context, req Models.AnthropicRequest) {
	inputTokens := estimateInputTokens(req)
	query := extractSearchQuery(req)
	if query == "" {
		anthropicError(c, http.StatusBadRequest, "invalid_request_error", "Cannot extract search query")
		return
	}

	// Get max_uses from tool
	maxUses := 5 // default
	for _, tool := range req.Tools {
		if tool.Name == "web_search" && tool.MaxUses > 0 {
			maxUses = tool.MaxUses
			break
		}
	}

	mcpReq := MCPRequest{
		ID:      fmt.Sprintf("web_search_tooluse_%s_%d_%s", uuid.NewString()[:22], time.Now().UnixMilli(), uuid.NewString()[:8]),
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params: MCPParams{
			Name:      "web_search",
			Arguments: MCPArguments{Query: query},
		},
	}

	jsonBytes, _ := json.Marshal(mcpReq)

	client := &http.Client{
		Transport: Utils.GetProxyTransport(),
		Timeout:   5 * time.Minute,
	}

	bearer, err := Utils.GetBearer()
	if err != nil {
		anthropicError(c, http.StatusInternalServerError, "api_error", "Failed to get bearer token")
		return
	}

	mcpURL := "https://q.us-east-1.amazonaws.com/mcp"
	httpReq, _ := http.NewRequest("POST", mcpURL, bytes.NewReader(jsonBytes))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+bearer)

	resp, err := client.Do(httpReq)
	if err != nil {
		Utils.ErrorLogger.Printf("MCP request failed: %v", err)
		anthropicError(c, http.StatusBadGateway, "api_error", "MCP service unavailable")
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var mcpResp MCPResponse
	if resp.StatusCode != http.StatusOK || json.Unmarshal(body, &mcpResp) != nil || mcpResp.Error != nil {
		anthropicError(c, http.StatusBadGateway, "api_error", "MCP request failed")
		return
	}

	// Parse search results
	var searchResults *WebSearchResults
	if mcpResp.Result != nil && len(mcpResp.Result.Content) > 0 {
		for _, content := range mcpResp.Result.Content {
			if content.Type == "text" {
				var results WebSearchResults
				if json.Unmarshal([]byte(content.Text), &results) == nil {
					searchResults = &results
					break
				}
			}
		}
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	toolUseID := "srvtoolu_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:32]
	msgID := "msg_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24]

	c.SSEvent("message_start", map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":            msgID,
			"type":          "message",
			"role":          "assistant",
			"model":         req.Model,
			"content":       []interface{}{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]int{
				"input_tokens":  inputTokens,
				"output_tokens": 0,
			},
		},
	})

	c.SSEvent("content_block_start", map[string]interface{}{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]interface{}{
			"id":    toolUseID,
			"type":  "server_tool_use",
			"name":  "web_search",
			"input": map[string]interface{}{},
		},
	})

	inputJSON, _ := json.Marshal(map[string]string{"query": query})
	c.SSEvent("content_block_delta", map[string]interface{}{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]interface{}{
			"type":         "input_json_delta",
			"partial_json": string(inputJSON),
		},
	})

	c.SSEvent("content_block_stop", map[string]interface{}{"type": "content_block_stop", "index": 0})

	// Build search result content blocks
	searchContent := []interface{}{}
	if searchResults != nil {
		limit := len(searchResults.Results)
		if maxUses > 0 && maxUses < limit {
			limit = maxUses
		}
		for i := 0; i < limit; i++ {
			result := searchResults.Results[i]
			searchContent = append(searchContent, map[string]interface{}{
				"type":              "web_search_result",
				"title":             result.Title,
				"url":               result.URL,
				"encrypted_content": result.Snippet,
				"page_age":          nil,
			})
		}
	}

	c.SSEvent("content_block_start", map[string]interface{}{
		"type":  "content_block_start",
		"index": 1,
		"content_block": map[string]interface{}{
			"type":        "web_search_tool_result",
			"tool_use_id": toolUseID,
			"content":     searchContent,
		},
	})

	c.SSEvent("content_block_stop", map[string]interface{}{"type": "content_block_stop", "index": 1})

	// Generate summary with results
	summary := fmt.Sprintf("Here are the search results for \"%s\":\n\n", query)
	if searchResults != nil && len(searchResults.Results) > 0 {
		limit := len(searchResults.Results)
		if maxUses > 0 && maxUses < limit {
			limit = maxUses
		}
		for i := 0; i < limit; i++ {
			result := searchResults.Results[i]
			summary += fmt.Sprintf("%d. **%s**\n", i+1, result.Title)
			if result.Snippet != "" {
				snippet := result.Snippet
				if len(snippet) > 200 {
					snippet = snippet[:200] + "..."
				}
				summary += fmt.Sprintf("   %s\n", snippet)
			}
			summary += fmt.Sprintf("   Source: %s\n\n", result.URL)
		}
	} else {
		summary += "No results found.\n"
	}

	c.SSEvent("content_block_start", map[string]interface{}{
		"type":  "content_block_start",
		"index": 2,
		"content_block": map[string]interface{}{
			"type": "text",
			"text": "",
		},
	})

	c.SSEvent("content_block_delta", map[string]interface{}{
		"type":  "content_block_delta",
		"index": 2,
		"delta": map[string]interface{}{
			"type": "text_delta",
			"text": summary,
		},
	})

	c.SSEvent("content_block_stop", map[string]interface{}{"type": "content_block_stop", "index": 2})

	outputTokens := countTokens(summary)
	c.SSEvent("message_delta", map[string]interface{}{
		"type": "message_delta",
		"delta": map[string]interface{}{
			"stop_reason":   "end_turn",
			"stop_sequence": nil,
		},
		"usage": map[string]int{"output_tokens": outputTokens},
	})

	c.SSEvent("message_stop", map[string]interface{}{"type": "message_stop"})
}
