package Utils

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"

	"kilocli2api/Models"
)

func ProcessQStreamToOpenAI(decoder *eventstream.Decoder, body io.Reader, payloadBuf []byte) (string, []Models.OpenAiTool, error) {
	type toolAccumulator struct {
		tool    *Models.OpenAiTool
		builder strings.Builder
	}

	var (
		fullContent  string
		accumulators = make(map[string]*toolAccumulator)
		order        []string
	)

	for {
		msg, err := decoder.Decode(body, payloadBuf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", nil, fmt.Errorf("decode error: %v", err)
		}

		var qMsg Models.QSSEResponse
		if err := json.Unmarshal(msg.Payload, &qMsg); err != nil {
			return "", nil, fmt.Errorf("unmarshal error: %v", err)
		}

		switch {
		case qMsg.Content != "":
			fullContent += qMsg.Content
		case qMsg.Reason != "":
			return "", nil, fmt.Errorf("%s: %s", qMsg.Reason, qMsg.Message)
		case qMsg.ToolUseId != "":
			acc, exists := accumulators[qMsg.ToolUseId]
			if !exists {
				acc = &toolAccumulator{
					tool: &Models.OpenAiTool{
						Index: len(order),
						Id:    qMsg.ToolUseId,
						Type:  "function",
						Function: Models.OpenAIFunction{
							Name: qMsg.Name,
						},
					},
				}
				accumulators[qMsg.ToolUseId] = acc
				order = append(order, qMsg.ToolUseId)
			}
			if qMsg.Name != "" {
				acc.tool.Function.Name = qMsg.Name
			}
			if qMsg.Input != "" {
				acc.builder.WriteString(qMsg.Input)
			}
		}
	}

	var toolCalls []Models.OpenAiTool
	for _, id := range order {
		acc := accumulators[id]
		raw := strings.TrimSpace(acc.builder.String())
		if raw == "" {
			raw = "{}"
		}
		if !json.Valid([]byte(raw)) {
			encoded, err := json.Marshal(raw)
			if err != nil {
				raw = "{}"
			} else {
				raw = string(encoded)
			}
		}
		acc.tool.Function.Arguments = json.RawMessage(raw)
		toolCalls = append(toolCalls, *acc.tool)
	}

	return fullContent, toolCalls, nil
}
