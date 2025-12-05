package API

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/google/uuid"

	"kilocli2api/Models"
	"kilocli2api/Utils"

	"github.com/gin-gonic/gin"
)

func ChatCompletions(c *gin.Context) {
	// Read raw body for debugging purposes
	rawBody, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
		return
	}
	// Restore the body for binding
	c.Request.Body = io.NopCloser(bytes.NewBuffer(rawBody))

	var req Models.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := Utils.ValidateChatCompletionRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Stream {
		handleStreamingRequest(c, req)
	} else {
		handleNonStreamingRequest(c, req)
	}
}

func handleNonStreamingRequest(c *gin.Context, req Models.ChatCompletionRequest) {
	incomingBytes, _ := json.Marshal(req)

	QRequest, err := Utils.MapOpenAiToAmazonQ(req, uuid.New().String(), ".")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to map OpenAI request to QAPIRequest"})
		Utils.LogRequestError(string(incomingBytes), QRequest)
		return
	}

	jsonBytes, err := json.Marshal(QRequest)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal QAPIRequest"})
		Utils.LogRequestError(string(incomingBytes), QRequest)
		return
	}

	qUrl := os.Getenv("AMAZON_Q_URL")
	client := &http.Client{Transport: Utils.GetProxyTransport()}

	for attempt := 0; attempt < 3; attempt++ {
		req2Q, _ := http.NewRequest("POST", qUrl, bytes.NewReader(jsonBytes))
		req2Q.Header.Set("user-agent", "aws-sdk-rust/1.3.10 ua/2.1 api/codewhispererstreaming/0.1.12842 os/macos lang/rust/1.88.0 md/appVersion-1.21.0 app/AmazonQ-For-CLI")
		req2Q.Header.Set("accept", "*/*")
		req2Q.Header.Set("accept-encoding", "gzip")
		req2Q.Header.Set("content-type", "application/x-amz-json-1.0")
		req2Q.Header.Set("x-amz-target", "AmazonCodeWhispererStreamingService.GenerateAssistantResponse")
		req2Q.Header.Set("x-amz-user-agent", "aws-sdk-rust/1.3.10 ua/2.1 api/codewhispererstreaming/0.1.12842 os/macos lang/rust/1.88.0 m/F app/AmazonQ-For-CLI")
		req2Q.Header.Set("x-amzn-codewhisperer-optout", "true")
		req2Q.Header.Set("amz-sdk-request", "attempt=1; max=3")
		req2Q.Header.Set("amz-sdk-invocation-id", uuid.NewString())

		bearer, err := Utils.GetBearer()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get bearer token"})
			Utils.NormalLogger.Println("Error getting bearer token:", err)
			return
		}
		req2Q.Header.Set("Authorization", "Bearer "+bearer)

		resp, err := client.Do(req2Q)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get response from Amazon Q API"})
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
			c.JSON(resp.StatusCode, gin.H{"error": string(bodyBytes)})
			Utils.ErrorLogger.Printf("Error response from Q API: %s", string(bodyBytes))
			return
		}

		decoder := eventstream.NewDecoder()
		payloadBuf := make([]byte, 1024*1024)

		fullContent, toolCalls, err := Utils.ProcessQStreamToOpenAI(decoder, resp.Body, payloadBuf)
		resp.Body.Close()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			Utils.NormalLogger.Println("Error:", err)
			return
		}

		finishReason := "stop"
		if len(toolCalls) > 0 {
			finishReason = "tool_calls"
		}

		message := Models.OpenAiMessage{
			Role: "assistant",
			Content: Models.MessageContent{
				IsString: true,
				String:   fullContent,
			},
			ToolCalls: toolCalls,
		}

		response := Models.ChatCompletionResponse{
			ID:      "chatcmpl-" + uuid.NewString(),
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   req.Model,
			Choices: []Models.Choice{
				{
					Index:        0,
					Message:      message,
					FinishReason: finishReason,
				},
			},
			Usage: Models.Usage{
				PromptTokens:     0,
				CompletionTokens: 0,
				TotalTokens:      0,
			},
		}

		c.JSON(http.StatusOK, response)
		return
	}
}

func handleStreamingRequest(c *gin.Context, req Models.ChatCompletionRequest) {
	incomingBytes, _ := json.Marshal(req)

	QRequest, err := Utils.MapOpenAiToAmazonQ(req, uuid.New().String(), ".")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to map OpenAI request to QAPIRequest"})
		Utils.LogRequestError(string(incomingBytes), QRequest)
		return
	}

	jsonBytes, err := json.Marshal(QRequest)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to marshal QAPIRequest"})
		Utils.LogRequestError(string(incomingBytes), QRequest)
		return
	}

	qUrl := os.Getenv("AMAZON_Q_URL")
	client := &http.Client{Transport: Utils.GetProxyTransport()}

	var resp *http.Response
	var bearer string
	for attempt := 0; attempt < 3; attempt++ {
		req2Q, _ := http.NewRequest("POST", qUrl, bytes.NewReader(jsonBytes))
		req2Q.Header.Set("user-agent", "aws-sdk-rust/1.3.10 ua/2.1 api/codewhispererstreaming/0.1.12842 os/macos lang/rust/1.88.0 md/appVersion-1.21.0 app/AmazonQ-For-CLI")
		req2Q.Header.Set("accept", "*/*")
		req2Q.Header.Set("accept-encoding", "gzip")
		req2Q.Header.Set("content-type", "application/x-amz-json-1.0")
		req2Q.Header.Set("x-amz-target", "AmazonCodeWhispererStreamingService.GenerateAssistantResponse")
		req2Q.Header.Set("x-amz-user-agent", "aws-sdk-rust/1.3.10 ua/2.1 api/codewhispererstreaming/0.1.12842 os/macos lang/rust/1.88.0 m/F app/AmazonQ-For-CLI")
		req2Q.Header.Set("x-amzn-codewhisperer-optout", "true")
		req2Q.Header.Set("amz-sdk-request", "attempt=1; max=3")
		req2Q.Header.Set("amz-sdk-invocation-id", uuid.NewString())

		bearer, err = Utils.GetBearer()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get bearer token"})
			Utils.NormalLogger.Println("Error getting bearer token:", err)
			return
		}
		req2Q.Header.Set("Authorization", "Bearer "+bearer)

		resp, err = client.Do(req2Q)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get response from Amazon Q API"})
			Utils.NormalLogger.Println("Error response from Q API:", err)
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
			c.JSON(resp.StatusCode, gin.H{"error": string(bodyBytes)})
			Utils.ErrorLogger.Printf("Error response from Q API: %s", string(bodyBytes))
			return
		}
		break
	}

	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {

		}
	}(resp.Body)

	// Set headers for SSE
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	// Stream the response back to the client
	decoder := eventstream.NewDecoder()

	// Buffer for message payloads - reused between decode calls
	payloadBuf := make([]byte, 1024*1024) // 1MB buffer

	// init var for sse response
	var id = "chatcmpl-" + uuid.NewString()
	var object = "chat.completion.chunk"
	var model = req.Model
	var created = time.Now().Unix()
	var index = 0
	var toolCallMap = make(map[string]int) // Track tool call indices
	var nextToolIndex = 0
	var isToolCalled bool
	var inThinking bool

	// Continuously decode messages while streaming
	messageCount := 0
	for {
		msg, err := decoder.Decode(resp.Body, payloadBuf)
		if err != nil {
			if err == io.EOF {
				break
			}
			// fmt.Printf("Decode error: %v\n", err)
			return
		}

		if messageCount == 0 {
			msg := Models.SSEResponse{
				ID:      id,
				Object:  object,
				Created: created,
				Model:   model,
				Choices: []Models.SSEChoice{
					{
						Index: index,
						Delta: Models.Delta{
							Role:    "assistant",
							Content: "",
						},
						FinishReason: nil,
					},
				},
			}
			c.SSEvent("", msg)
		}

		// map to QSSEResponse
		var qMsg Models.QSSEResponse
		if err := json.Unmarshal(msg.Payload, &qMsg); err != nil {
			c.JSON(resp.StatusCode, gin.H{"error": err.Error()})
			Utils.NormalLogger.Printf("Unmarshal error: %v\n\n", err)
			return
		}

		// Handle different event types
		if qMsg.Content != "" {
			content := qMsg.Content
			for len(content) > 0 {
				if !inThinking {
					if idx := strings.Index(content, "<thinking>"); idx >= 0 {
						if idx > 0 {
							msg := Models.SSEResponse{
								ID:      id,
								Object:  object,
								Created: created,
								Model:   model,
								Choices: []Models.SSEChoice{
									{
										Index: index,
										Delta: Models.Delta{
											Content: content[:idx],
										},
										FinishReason: nil,
									},
								},
							}
							c.SSEvent("", msg)
						}
						inThinking = true
						content = content[idx+10:]
					} else {
						msg := Models.SSEResponse{
							ID:      id,
							Object:  object,
							Created: created,
							Model:   model,
							Choices: []Models.SSEChoice{
								{
									Index: index,
									Delta: Models.Delta{
										Content: content,
									},
									FinishReason: nil,
								},
							},
						}
						c.SSEvent("", msg)
						break
					}
				} else {
					if idx := strings.Index(content, "</thinking>"); idx >= 0 {
						if idx > 0 {
							msg := Models.SSEResponse{
								ID:      id,
								Object:  object,
								Created: created,
								Model:   model,
								Choices: []Models.SSEChoice{
									{
										Index: index,
										Delta: Models.Delta{
											Reasoning: content[:idx],
										},
										FinishReason: nil,
									},
								},
							}
							c.SSEvent("", msg)
						}
						inThinking = false
						content = content[idx+11:]
					} else {
						msg := Models.SSEResponse{
							ID:      id,
							Object:  object,
							Created: created,
							Model:   model,
							Choices: []Models.SSEChoice{
								{
									Index: index,
									Delta: Models.Delta{
										Reasoning: content,
									},
									FinishReason: nil,
								},
							},
						}
						c.SSEvent("", msg)
						break
					}
				}
			}
		} else if qMsg.Reason != "" {
			// InvalidStateEvent
			c.SSEvent("", gin.H{
				"error":   qMsg.Reason,
				"message": qMsg.Message,
			})
		} else if qMsg.ConversationId != "" {
			// do nothing
		} else if qMsg.ToolUseId != "" {
			// ToolUseEvent

			// Determine the tool index for this tool call
			toolIdx, exists := toolCallMap[qMsg.ToolUseId]
			if !exists {
				// New tool call - assign the next index
				toolIdx = nextToolIndex
				toolCallMap[qMsg.ToolUseId] = toolIdx
				nextToolIndex++
			}

			msg := Models.SSEResponse{
				ID:      id,
				Object:  object,
				Created: created,
				Model:   model,
				Choices: []Models.SSEChoice{
					{
						Index: index,
						Delta: Models.Delta{
							ToolCalls: []Models.SSEOpenAiTool{
								{
									Index: toolIdx,
									Function: Models.SSEOpenAIFunction{
										Arguments: qMsg.Input,
										Name:      qMsg.Name,
									},
									Id:   qMsg.ToolUseId,
									Type: "function",
								},
							},
						},
						FinishReason: nil,
					},
				},
			}
			isToolCalled = true
			c.SSEvent("", msg)

		}

		messageCount++
	}

	// Determine finish reason based on whether there were tool calls
	var stopReason string
	if isToolCalled {
		stopReason = "tool_calls"
	} else {
		stopReason = "stop"
	}

	msg := Models.SSEResponse{
		ID:      id,
		Object:  object,
		Created: created,
		Model:   model,
		Choices: []Models.SSEChoice{
			{
				Index:        index,
				Delta:        Models.Delta{},
				FinishReason: &stopReason,
			},
		},
	}

	c.SSEvent("", msg)
	c.SSEvent("", "[DONE]")
}
