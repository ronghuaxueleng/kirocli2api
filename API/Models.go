package API

import (
	"bytes"
	"encoding/json"
	"io"
	"kilocli2api/Models"
	"kilocli2api/Utils"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func ListModels(c *gin.Context) {
	qModels, err := fetchQModels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch models"})
		return
	}

	authHeader := c.GetHeader("Authorization")
	apiKey := c.GetHeader("x-api-key")
	isAnthropic := apiKey != "" || !strings.HasPrefix(authHeader, "Bearer ")

	if isAnthropic {
		handleAnthropicModels(c, qModels)
	} else {
		handleOpenAIModels(c, qModels)
	}
}

func fetchQModels() (*Models.QModelsResponse, error) {
	reqBody := map[string]string{"origin": "KIRO_CLI"}
	jsonBytes, _ := json.Marshal(reqBody)

	qUrl := "https://q.us-east-1.amazonaws.com?origin=KIRO_CLI"
	httpReq, _ := http.NewRequest("POST", qUrl, bytes.NewReader(jsonBytes))

	httpReq.Header.Set("user-agent", "aws-sdk-rust/1.3.10 ua/2.1 api/codewhispererstreaming/0.1.12842 os/macos lang/rust/1.88.0 md/appVersion-1.23.1 app/AmazonQ-For-CLI")
	httpReq.Header.Set("Content-Type", "application/x-amz-json-1.0")
	httpReq.Header.Set("x-amz-target", "AmazonCodeWhispererService.ListAvailableModels")
	httpReq.Header.Set("x-amz-user-agent", "aws-sdk-rust/1.3.10 ua/2.1 api/codewhispererstreaming/0.1.12842 os/macos lang/rust/1.88.0 m/F app/AmazonQ-For-CLI")
	httpReq.Header.Set("x-amzn-codewhisperer-optout", "true")
	httpReq.Header.Set("amz-sdk-request", "attempt=1; max=3")
	httpReq.Header.Set("amz-sdk-invocation-id", uuid.NewString())

	bearer, err := Utils.GetBearer()
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+bearer)

	client := &http.Client{Transport: Utils.GetProxyTransport()}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	var qResp Models.QModelsResponse
	err = json.Unmarshal(bodyBytes, &qResp)
	if err != nil {
		return nil, err
	}
	return &qResp, nil
}

func handleOpenAIModels(c *gin.Context, qModels *Models.QModelsResponse) {
	data := make([]Models.OpenAIModel, 0, len(qModels.Models)*2)
	for _, m := range qModels.Models {
		data = append(data, Models.OpenAIModel{
			ID:      m.ModelID,
			Object:  "model",
			Created: 1145141919,
			OwnedBy: "anthropic",
		}, Models.OpenAIModel{
			ID:      m.ModelID + "-thinking",
			Object:  "model",
			Created: 1145141919,
			OwnedBy: "anthropic",
		})
	}
	c.JSON(http.StatusOK, Models.OpenAIModelsResponse{Object: "list", Data: data})
}

func handleAnthropicModels(c *gin.Context, qModels *Models.QModelsResponse) {
	data := make([]Models.AnthropicModel, 0, len(qModels.Models)*2)
	for _, m := range qModels.Models {
		data = append(data, Models.AnthropicModel{
			ID:          m.ModelID,
			Type:        "model",
			DisplayName: m.ModelName,
			CreatedAt:   "2006-04-16T06:58:39Z",
		}, Models.AnthropicModel{
			ID:          m.ModelID + "-thinking",
			Type:        "model",
			DisplayName: m.ModelName + " (Thinking)",
			CreatedAt:   "2006-04-16T06:58:39Z",
		})
	}
	firstID := ""
	lastID := ""
	if len(data) > 0 {
		firstID = data[0].ID
		lastID = data[len(data)-1].ID
	}
	c.JSON(http.StatusOK, Models.AnthropicModelsResponse{Data: data, HasMore: false, FirstID: firstID, LastID: lastID})
}
