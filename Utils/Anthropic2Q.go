package Utils

import (
	"encoding/json"
	"github.com/google/uuid"
	"kilocli2api/Models"
)

// MapAnthropicToAmazonQ converts an Anthropic API request to Amazon Q API format.
// It handles conversation history, tool definitions, system messages, and ensures
// proper message alternation between user and assistant roles as required by Q API.
func MapAnthropicToAmazonQ(req Models.AnthropicRequest, conversationID string, currentWorkingDir string) (Models.QAPIRequest, error) {
	var Output Models.QAPIRequest

	// Initialize conversation metadata and environment state
	Output.ConversationState.ConversationID = conversationID
	Output.ConversationState.ChatTriggerType = "MANUAL"
	Output.ConversationState.CurrentMessage.UserInputMessage.Origin = "KIRO_CLI"
	Output.ConversationState.CurrentMessage.UserInputMessage.ModelID = ModelMapping(req.Model)

	var qEnvState = Models.QEnvState{
		OperatingSystem:         "macos",
		CurrentWorkingDirectory: currentWorkingDir,
	}
	Output.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.EnvState = qEnvState

	// Convert Anthropic tool definitions to Q API format
	longToolDocs := ""
	var QTools []Models.QTool
	for _, anthropicTool := range req.Tools {
		var qTool Models.QTool

		if len(anthropicTool.Description) > 10000 {
			longToolDocs = longToolDocs + "--- TOOL DOCUMENTATION BEGIN ---\nTool name: " + anthropicTool.Name + "\nFull Description: " + anthropicTool.Description + "\n--- TOOL DOCUMENTATION END ---\n\n"
			qTool.ToolSpecification = Models.QToolSpecification{
				Name:        anthropicTool.Name,
				Description: "See tool documentation section.",
				InputSchema: Models.QInputSchema{
					JSON: anthropicTool.InputSchema,
				},
			}
		} else {
			qTool.ToolSpecification = Models.QToolSpecification{
				Name:        anthropicTool.Name,
				Description: anthropicTool.Description,
				InputSchema: Models.QInputSchema{
					JSON: anthropicTool.InputSchema,
				},
			}
		}
		QTools = append(QTools, qTool)
	}
	if len(QTools) > 0 {
		Output.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools = &QTools
	}

	// Convert system message to conversation history
	// Q API doesn't have a dedicated system field, so we inject it as a user message
	// followed by an empty assistant acknowledgment
	var qHistories []Models.QHistoryItem

	if req.System.GetString() != "" {
		qHistories = append(qHistories, Models.QHistoryItem{
			UserInputMessage: &Models.QUserInputMessageHistory{
				Content: "--- SYSTEM PROMPT BEGIN ---\n" + ensureNonEmptyContent(req.System.GetString()) + "\n--- SYSTEM PROMPT END ---\n\n",
				UserInputMessageContext: Models.QUserInputMessageContext{
					EnvState: qEnvState,
				},
				Origin: "KIRO_CLI",
			},
		})

		// Add empty assistant response to maintain alternating pattern
		qHistories = append(qHistories, Models.QHistoryItem{
			AssistantResponseMessage: &Models.QAssistantResponseMessage{
				MessageID: uuid.New().String(),
				Content:   "-",
			},
		})
	}

	// Process all messages
	for i := range len(req.Messages) {
		anthropicMsg := req.Messages[i]
		if anthropicMsg.Role == "assistant" {
			// Handle simple text assistant messages
			if anthropicMsg.Content.IsString {
				qHistories = append(qHistories, Models.QHistoryItem{
					AssistantResponseMessage: &Models.QAssistantResponseMessage{
						MessageID: uuid.New().String(),
						Content:   ensureNonEmptyContent(anthropicMsg.Content.String),
					},
				})
			} else {
				// Handle complex assistant messages with multiple content blocks (text + tool uses)
				qHistoryItem := Models.QHistoryItem{
					AssistantResponseMessage: &Models.QAssistantResponseMessage{
						MessageID: uuid.New().String(),
					},
				}

				var toolUses []Models.QToolUse
				for i := range anthropicMsg.Content.Blocks {
					singleBlockMsg := anthropicMsg.Content.Blocks[i]
					switch singleBlockMsg.Type {
					case "text":
						qHistoryItem.AssistantResponseMessage.Content += ensureNonEmptyContent(singleBlockMsg.Text)
					case "tool_use":
						toolUses = append(toolUses, Models.QToolUse{
							ToolUseID: singleBlockMsg.ID,
							Input:     singleBlockMsg.Input,
							Name:      singleBlockMsg.Name,
						})
					default:
						if singleBlockMsg.Text != "" {
							qHistoryItem.AssistantResponseMessage.Content += ensureNonEmptyContent(singleBlockMsg.Text)
						}
					}
				}
				if len(toolUses) > 0 {
					qHistoryItem.AssistantResponseMessage.ToolUses = &toolUses
				}

				qHistoryItem.AssistantResponseMessage.Content = ensureNonEmptyContent(qHistoryItem.AssistantResponseMessage.Content)
				qHistories = append(qHistories, qHistoryItem)
			}
		} else if anthropicMsg.Role == "user" {
			// Handle simple text user messages
			if anthropicMsg.Content.IsString {
				qHistories = append(qHistories, Models.QHistoryItem{
					UserInputMessage: &Models.QUserInputMessageHistory{
						Content: ensureNonEmptyContent(anthropicMsg.Content.String),
						UserInputMessageContext: Models.QUserInputMessageContext{
							EnvState: qEnvState,
						},
						Origin: "KIRO_CLI",
					},
				})
			} else {
				// Handle complex user messages with multiple content blocks (text + tool results)
				qHistoryItem := Models.QHistoryItem{
					UserInputMessage: &Models.QUserInputMessageHistory{
						UserInputMessageContext: Models.QUserInputMessageContext{
							EnvState: qEnvState,
						},
						Origin: "KIRO_CLI",
					},
				}

				var toolResults []Models.QToolResultItem
				for i := range anthropicMsg.Content.Blocks {
					singleBlockMsg := anthropicMsg.Content.Blocks[i]
					switch singleBlockMsg.Type {
					case "text":
						qHistoryItem.UserInputMessage.Content += ensureNonEmptyContent(singleBlockMsg.Text)
					case "tool_result":
						qToolResultItem := Models.QToolResultItem{
							ToolUseID: singleBlockMsg.ToolUseID,
							Status:    "success",
						}

						// Handle content based on its type
						switch content := singleBlockMsg.Content.(type) {
						case string:
							var qToolResultContent Models.QToolResultContent
							qToolResultContent.Text = ensureNonEmptyContent(content)
							qToolResultItem.Content = append(qToolResultItem.Content, qToolResultContent)
						case []interface{}:
							// Handle array of content blocks
							for _, block := range content {
								if blockMap, ok := block.(map[string]interface{}); ok {
									if text, ok := blockMap["text"].(string); ok {
										var qToolResultContent Models.QToolResultContent
										qToolResultContent.Text = ensureNonEmptyContent(text)
										qToolResultItem.Content = append(qToolResultItem.Content, qToolResultContent)
									}
								}
							}
						}

						toolResults = append(toolResults, qToolResultItem)
					default:
						if singleBlockMsg.Text != "" {
							qHistoryItem.AssistantResponseMessage.Content += ensureNonEmptyContent(singleBlockMsg.Text)
						}
					}
				}

				if len(toolResults) > 0 {
					qHistoryItem.UserInputMessage.UserInputMessageContext.ToolResults = &toolResults
				}

				qHistoryItem.UserInputMessage.Content = ensureNonEmptyContent(qHistoryItem.UserInputMessage.Content)
				qHistories = append(qHistories, qHistoryItem)
			}
		}
	}

	// Q API requires strict alternation between user and assistant messages.
	// Insert empty messages between consecutive messages of the same role without dropping any originals.
	var qHistoriesFinal []Models.QHistoryItem
	var prevRole string
	for i := range qHistories {
		currentHistory := qHistories[i]
		currentRole := ""
		if currentHistory.UserInputMessage != nil {
			currentRole = "user"
		} else if currentHistory.AssistantResponseMessage != nil {
			currentRole = "assistant"
		}

		if prevRole != "" && currentRole == prevRole {
			if currentRole == "user" {
				// Two consecutive user messages - insert empty assistant message
				var emptyAssisstantMsg = Models.QHistoryItem{
					AssistantResponseMessage: &Models.QAssistantResponseMessage{
						MessageID: uuid.New().String(),
						Content:   "-",
					},
				}
				qHistoriesFinal = append(qHistoriesFinal, emptyAssisstantMsg)
			} else if currentRole == "assistant" {
				// Two consecutive assistant messages - insert empty user message
				var emptyUserMsg = Models.QHistoryItem{
					UserInputMessage: &Models.QUserInputMessageHistory{
						Content: "-",
						UserInputMessageContext: Models.QUserInputMessageContext{
							EnvState: qEnvState,
						},
						Origin: "KIRO_CLI",
					},
				}
				qHistoriesFinal = append(qHistoriesFinal, emptyUserMsg)
			}
		}

		qHistoriesFinal = append(qHistoriesFinal, currentHistory)
		prevRole = currentRole
	}

	if len(qHistoriesFinal) == 0 {
		Output.ConversationState.CurrentMessage.UserInputMessage.Content = "-"
		Output.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.EnvState = qEnvState
		Output.ConversationState.History = qHistoriesFinal
		return Output, nil
	}

	qHistories = qHistoriesFinal

	// Process the last message in the history
	lastHistoryItem := qHistories[len(qHistories)-1]
	if lastHistoryItem.UserInputMessage != nil { // last item is user message
		Output.ConversationState.CurrentMessage.UserInputMessage.Content = longToolDocs + lastHistoryItem.UserInputMessage.Content
		Output.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.ToolResults = lastHistoryItem.UserInputMessage.UserInputMessageContext.ToolResults
		Output.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.EnvState = qEnvState

		qHistories = qHistories[:len(qHistories)-1]
	} else if lastHistoryItem.AssistantResponseMessage != nil { // last item is assistant message
		// for current message, let be empty
		Output.ConversationState.CurrentMessage.UserInputMessage.Content = longToolDocs
		Output.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.EnvState = qEnvState

	}

	Output.ConversationState.History = qHistories

	// Ensure content is never empty (Q API requirement)
	Output.ConversationState.CurrentMessage.UserInputMessage.Content = ensureNonEmptyContent(Output.ConversationState.CurrentMessage.UserInputMessage.Content)

	return Output, nil
}

func isValidJSON(data interface{}) bool {
	var jsonBytes []byte

	switch v := data.(type) {
	case string:
		jsonBytes = []byte(v)
	case []byte:
		jsonBytes = v
	default:
		return false
	}

	return json.Valid(jsonBytes)
}
