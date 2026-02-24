/*
 *                        _oo0oo_
 *                       o8888888o
 *                       88" . "88
 *                       (| -_- |)
 *                       0\  =  /0
 *                     ___/`---'\___
 *                   .' \\|     |// '.
 *                  / \\|||  :  |||// \
 *                 / _||||| -:- |||||- \
 *                |   | \\\  - /// |   |
 *                | \_|  ''\---/''  |_/ |
 *                \  .-\__  '-'  ___/-. /
 *              ___'. .'  /--.--\  `. .'___
 *           ."" '<  `.___\_<|>_/___.' >' "".
 *          | | :  `- \`.;`\ _ /`;.`/ - ` : | |
 *          \  \ `_.   \_ __\ /__ _/   .-` /  /
 *      =====`-.____`.___ \_____/___.-`___.-'=====
 *                        `=---='
 *
 *
 *      ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
 *
 *            佛祖保佑       永不宕机     永无BUG
 */

package Utils

import (
	"encoding/json"
	"errors"
	"kilocli2api/Models"
	"strings"

	"github.com/google/uuid"
)

func ensureToolResults(ctx *Models.QUserInputMessageContext) *[]Models.QToolResultItem {
	if ctx.ToolResults == nil {
		slice := make([]Models.QToolResultItem, 0)
		ctx.ToolResults = &slice
	}
	return ctx.ToolResults
}

func MapOpenAiToAmazonQ(req Models.ChatCompletionRequest, conversationID string, currentWorkingDir string) (Models.QAPIRequest, error) {
	var Output Models.QAPIRequest

	// Constants
	Output.ConversationState.ConversationID = conversationID
	Output.ConversationState.ChatTriggerType = "MANUAL"
	Output.ConversationState.CurrentMessage.UserInputMessage.Origin = "KIRO_CLI"
	Output.ConversationState.CurrentMessage.UserInputMessage.ModelID = strings.TrimSuffix(req.Model, "-thinking")

	var qEnvState = Models.QEnvState{
		OperatingSystem:         "macos",
		CurrentWorkingDirectory: currentWorkingDir,
	}
	Output.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.EnvState = qEnvState

	// Tools
	var QTools []Models.QTool
	for _, openAiTool := range req.Tools {
		var QTool Models.QTool
		QTool.ToolSpecification = Models.QToolSpecification{
			Name:        openAiTool.Function.Name,
			Description: openAiTool.Function.Description,
			InputSchema: Models.QInputSchema{
				JSON: openAiTool.Function.Parameters,
			},
		}
		QTools = append(QTools, QTool)
	}
	if len(QTools) > 0 {
		Output.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.Tools = &QTools
	}

	var OpenAIMessages = req.Messages

	var OutputHistoryIndex = 0
	var lastUserIndexOfOutput = -1
	var lastAssistantIndexOfOutput = -1

	var qHistories []Models.QHistoryItem

	for i := range OpenAIMessages { // for loop forwards
		msg := OpenAIMessages[i]

		if msg.Role == "system" || msg.Role == "developer" {
			var QHistoryItem Models.QHistoryItem
			QHistoryItem.UserInputMessage = &Models.QUserInputMessageHistory{
				Content: ensureNonEmptyContent(msg.Content.GetString()),
				UserInputMessageContext: Models.QUserInputMessageContext{
					EnvState: qEnvState,
				},
				Origin: "KIRO_CLI",
			}
			qHistories = append(qHistories, QHistoryItem)
			lastUserIndexOfOutput = OutputHistoryIndex
			OutputHistoryIndex++
		} else if msg.Role == "user" { // history msg
			var QHistoryItem Models.QHistoryItem

			content := ensureNonEmptyContent(msg.Content.GetString())

			var qImages []Models.QImage
			for _, block := range msg.Content.Contents {
				if block.Type == "image_url" {
					qImages = append(qImages, Models.QImage{
						Format: getExtensionFromBase64Data(block.ImageUrl.Url),
						Source: Models.QImageSource{
							Bytes: removeBase64Header(block.ImageUrl.Url),
						},
					})
				}
			}

			QHistoryItem.UserInputMessage = &Models.QUserInputMessageHistory{
				Content: content,
				QImage:  qImages,
				UserInputMessageContext: Models.QUserInputMessageContext{
					EnvState: qEnvState,
				},
				Origin: "KIRO_CLI",
			}
			qHistories = append(qHistories, QHistoryItem)
			lastUserIndexOfOutput = OutputHistoryIndex
			OutputHistoryIndex++
		} else if msg.Role == "assistant" {
			lastAssistantIndexOfOutput = i

			var QHistoryItem Models.QHistoryItem

			content := ensureNonEmptyContent(msg.Content.GetString())

			QHistoryItem.AssistantResponseMessage = &Models.QAssistantResponseMessage{
				MessageID: uuid.New().String(),
				Content:   content,
			}
			// for loop the tool calls
			var qToolUses []Models.QToolUse
			for _, openAiToolCall := range msg.ToolCalls {
				var qToolUse Models.QToolUse
				qToolUse.ToolUseID = openAiToolCall.Id
				qToolUse.Name = openAiToolCall.Function.Name
				if len(strings.TrimSpace(string(openAiToolCall.Function.Arguments))) == 0 {
					qToolUse.Input = "-"
				} else if json.Valid(openAiToolCall.Function.Arguments) {
					var jsonArgs interface{}
					if err := json.Unmarshal(openAiToolCall.Function.Arguments, &jsonArgs); err != nil {
						NormalLogger.Println("Error unmarshaling tool call arguments:", err)
						continue
					}
					// Handle double-encoded JSON string
					if str, ok := jsonArgs.(string); ok {
						if err := json.Unmarshal([]byte(str), &jsonArgs); err != nil {
							NormalLogger.Println("Error unmarshaling nested tool call arguments:", err)
							continue
						}
					}
					qToolUse.Input = jsonArgs
				} else {
					qToolUse.Input = string(openAiToolCall.Function.Arguments)
				}

				qToolUses = append(qToolUses, qToolUse)
			}
			if len(qToolUses) > 0 {
				QHistoryItem.AssistantResponseMessage.ToolUses = &qToolUses
			}

			// append into history
			qHistories = append(qHistories, QHistoryItem)
			lastAssistantIndexOfOutput = OutputHistoryIndex
			OutputHistoryIndex++
		} else if msg.Role == "tool" {
			// Three senario:
			// 1. last user > last assis, normal case
			// 2. last assis > last user, need to add a blank user msg
			// 3. have -1, ???

			if lastUserIndexOfOutput > lastAssistantIndexOfOutput {
				var qToolResultItem Models.QToolResultItem
				qToolResultItem.ToolUseID = msg.ToolCallID
				qToolResultItem.Status = "success"

				// need to check if msg.Content is string or json
				if json.Valid(msg.Content.GetBytes()) { // is json
					var rawMsg json.RawMessage = msg.Content.GetBytes()
					var qToolResultContent Models.QToolResultContent
					qToolResultContent.JSON = &rawMsg
					qToolResultItem.Content = append(qToolResultItem.Content, qToolResultContent)
				} else { // is string
					var qToolResultContent Models.QToolResultContent
					qToolResultContent.Text = ensureNonEmptyContent(msg.Content.GetString())
					qToolResultItem.Content = append(qToolResultItem.Content, qToolResultContent)
				}

				toolResults := ensureToolResults(&qHistories[lastUserIndexOfOutput].UserInputMessage.UserInputMessageContext)
				*toolResults = append(*toolResults, qToolResultItem)
			} else if lastAssistantIndexOfOutput > lastUserIndexOfOutput {
				// need to add a blank user msg
				var emptyUserMsg = Models.QHistoryItem{
					UserInputMessage: &Models.QUserInputMessageHistory{
						Content: "-",
						UserInputMessageContext: Models.QUserInputMessageContext{
							EnvState: qEnvState,
						},
						Origin: "KIRO_CLI",
					},
				}
				qHistories = append(qHistories, emptyUserMsg)
				lastUserIndexOfOutput = OutputHistoryIndex
				OutputHistoryIndex++

				// make Models.QToolResultItem
				var qToolResultItem Models.QToolResultItem
				qToolResultItem.ToolUseID = msg.ToolCallID
				qToolResultItem.Status = "success"

				// need to check if msg.Content is string or json
				if json.Valid(msg.Content.GetBytes()) { // is json
					var rawMsg json.RawMessage = msg.Content.GetBytes()
					var qToolResultContent Models.QToolResultContent
					qToolResultContent.JSON = &rawMsg
					qToolResultItem.Content = append(qToolResultItem.Content, qToolResultContent)
				} else { // is string
					var qToolResultContent Models.QToolResultContent
					qToolResultContent.Text = ensureNonEmptyContent(msg.Content.GetString())
					qToolResultItem.Content = append(qToolResultItem.Content, qToolResultContent)
				}

				// Add into the new user msg's tool results
				toolResults := ensureToolResults(&qHistories[lastUserIndexOfOutput].UserInputMessage.UserInputMessageContext)
				*toolResults = append(*toolResults, qToolResultItem)
			}

		}
	}

	// Fine the LAST message in qHistories
	var lastIndex = len(qHistories) - 1
	var lastQHistoryItem = qHistories[lastIndex]

	// assign it to current message
	if lastQHistoryItem.UserInputMessage != nil {
		qHistories = qHistories[:lastIndex] // pop

		Output.ConversationState.CurrentMessage.UserInputMessage.Content = ensureNonEmptyContent(lastQHistoryItem.UserInputMessage.Content)
		if lastQHistoryItem.UserInputMessage.UserInputMessageContext.ToolResults != nil {
			Output.ConversationState.CurrentMessage.UserInputMessage.UserInputMessageContext.ToolResults = lastQHistoryItem.UserInputMessage.UserInputMessageContext.ToolResults
		}
		Output.ConversationState.CurrentMessage.UserInputMessage.QImage = lastQHistoryItem.UserInputMessage.QImage
	} else if lastQHistoryItem.AssistantResponseMessage != nil {
		// current msg is blank
		Output.ConversationState.CurrentMessage.UserInputMessage.Content = "-"
	} else {
		// error
		NormalLogger.Println("Error: last message in qHistories is neither user nor assistant")
		return Models.QAPIRequest{}, errors.New("last message in qHistories is neither user nor assistant")
	}

	// Final check: check if there are 2 continuous user messages or assistant messages, if yes, add a empty message in between
	var qHistoriesFinal []Models.QHistoryItem
	for i := 1; i <= len(qHistories)-1; i++ {
		if qHistories[i].UserInputMessage != nil && qHistories[i-1].UserInputMessage != nil {
			qHistoriesFinal = append(qHistoriesFinal, qHistories[i-1])
			var emptyAssisstantMsg = Models.QHistoryItem{
				AssistantResponseMessage: &Models.QAssistantResponseMessage{
					MessageID: uuid.New().String(),
					Content:   "-",
				},
			}
			qHistoriesFinal = append(qHistoriesFinal, emptyAssisstantMsg)
		} else if qHistories[i].AssistantResponseMessage != nil && qHistories[i-1].AssistantResponseMessage != nil {
			qHistoriesFinal = append(qHistoriesFinal, qHistories[i-1])
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
		} else {
			qHistoriesFinal = append(qHistoriesFinal, qHistories[i-1])
		}
	}
	// append the last item
	if len(qHistories) > 0 {
		qHistoriesFinal = append(qHistoriesFinal, qHistories[len(qHistories)-1])
		if qHistories[len(qHistories)-1].UserInputMessage != nil { // fuck amazon
			var emptyAssisstantMsg = Models.QHistoryItem{
				AssistantResponseMessage: &Models.QAssistantResponseMessage{
					MessageID: uuid.New().String(),
					Content:   "-",
				},
			}
			qHistoriesFinal = append(qHistoriesFinal, emptyAssisstantMsg)
		}
	}

	Output.ConversationState.History = qHistoriesFinal

	// Fuck Q api
	Output.ConversationState.CurrentMessage.UserInputMessage.Content = ensureNonEmptyContent(Output.ConversationState.CurrentMessage.UserInputMessage.Content)

	// Check if it is thinking request
	if (req.Reasoning != "none" && req.Reasoning != "") || strings.HasSuffix(req.Model, "-thinking") {
		Output.ConversationState.CurrentMessage.UserInputMessage.Content += "<antml:thinking_mode>interleaved</antml:thinking_mode><antml:max_thinking_length>16000</antml:max_thinking_length>"
	}

	return Output, nil
}
