package API

import (
	"bytes"
	"encoding/json"
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
		httpReq.Header.Set("user-agent", "aws-sdk-rust/1.3.10 ua/2.1 api/codewhispererstreaming/0.1.12842 os/macos lang/rust/1.88.0 md/appVersion-1.21.0 app/AmazonQ-For-CLI")
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
		httpReq.Header.Set("user-agent", "aws-sdk-rust/1.3.10 ua/2.1 api/codewhispererstreaming/0.1.12842 os/macos lang/rust/1.88.0 md/appVersion-1.21.0 app/AmazonQ-For-CLI")
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
