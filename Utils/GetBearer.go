package Utils

import (
	"bytes"
	"crypto/tls"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"io"
	"kilocli2api/Models"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

var initDoOnce = &sync.Once{}

var RefreshTokens []Models.RefreshToken
var ActiveTokens []int
var tokenMutex sync.RWMutex
var csvMutex sync.Mutex
var tokenIndex int
var csvPath string
var activeTokenCount int
var nextRefreshTokenIndex int
var maxRefreshAttempt int

func getProxyTransport() *http.Transport {
	transport := &http.Transport{
		TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
	if proxyURL := os.Getenv("PROXY_URL"); proxyURL != "" {
		if proxy, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(proxy)
		}
	}
	return transport
}

func loadAccountsFromCSV(csvPath string) {
	for {
		if _, err := os.Stat(csvPath); os.IsNotExist(err) {
			NormalLogger.Printf("CSV file does not exist: %s, waiting...\n", csvPath)
			time.Sleep(10 * time.Second)
			continue
		}
		break
	}

	file, err := os.Open(csvPath)
	if err != nil {
		panic(fmt.Sprintf("Failed to open CSV file: %v", err))
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			panic(fmt.Sprintf("Failed to close CSV file: %v", err))
		}
	}(file)

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		panic(fmt.Sprintf("Failed to read CSV file: %v", err))
	}

	for i, record := range records {
		if i == 0 || len(record) < 4 {
			continue
		}
		if strings.TrimSpace(record[0]) == "True" {
			RefreshTokens = append(RefreshTokens, Models.RefreshToken{
				Token:        strings.TrimSpace(record[1]),
				ClientId:     strings.TrimSpace(record[2]),
				ClientSecret: strings.TrimSpace(record[3]),
			})
		}
	}

	if len(RefreshTokens) == 0 {
		panic("No enabled accounts found in CSV")
	}
}

type APIAccount struct {
	ID           int    `json:"id"`
	RefreshToken string `json:"refresh_token"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

func loadAccountsFromAPI(apiURL, apiToken string, count int) {
	url := fmt.Sprintf("%s?count=%d", apiURL, count)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(fmt.Sprintf("Failed to create API request: %v", err))
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiToken))

	client := &http.Client{
		Transport: getProxyTransport(),
		Timeout:   30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		panic(fmt.Sprintf("Failed to fetch accounts from API: %v", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		panic(fmt.Sprintf("API returned status %d: %s", resp.StatusCode, string(body)))
	}

	var accounts []APIAccount
	if err := json.NewDecoder(resp.Body).Decode(&accounts); err != nil {
		panic(fmt.Sprintf("Failed to parse API response: %v", err))
	}

	for _, acc := range accounts {
		RefreshTokens = append(RefreshTokens, Models.RefreshToken{
			Token:        acc.RefreshToken,
			ClientId:     acc.ClientID,
			ClientSecret: strings.ReplaceAll(acc.ClientSecret, "\r", ""),
		})
	}

	if len(RefreshTokens) == 0 {
		panic("No accounts received from API")
	}
}

func DisableToken(accessToken string, reason string) {
	tokenMutex.Lock()
	defer tokenMutex.Unlock()

	for i, idx := range ActiveTokens {
		if RefreshTokens[idx].AccessToken.Token == accessToken {
			NormalLogger.Printf("Disabling active token %d, reason: %s\n", idx, reason)
			RefreshTokens[idx].AccessToken.ExpiresAt = 0
			updateCSVEnabled(RefreshTokens[idx].Token)

			if nextRefreshTokenIndex < len(RefreshTokens) {
				var newToken Models.AccessToken
				var err error
				for attempt := 0; attempt < maxRefreshAttempt; attempt++ {
					newToken, err = GetAccessTokenFromRefreshToken(RefreshTokens[nextRefreshTokenIndex])
					if err == nil {
						break
					}
					NormalLogger.Printf("Failed to get new access token (attempt %d/%d): %v\n", attempt+1, maxRefreshAttempt, err)
				}
				if err != nil {
					ActiveTokens = append(ActiveTokens[:i], ActiveTokens[i+1:]...)
				} else {
					RefreshTokens[nextRefreshTokenIndex].AccessToken = newToken
					ActiveTokens[i] = nextRefreshTokenIndex
					NormalLogger.Printf("Rotated to new token from refresh token %d\n", nextRefreshTokenIndex)
				}
				nextRefreshTokenIndex++
			} else {
				go fetchAndAddNewToken()
				ActiveTokens = append(ActiveTokens[:i], ActiveTokens[i+1:]...)
				NormalLogger.Printf("No more refresh tokens in pool, fetching new token\n")
			}
			break
		}
	}
}

func fetchAndAddNewToken() {
	accountSource := os.Getenv("ACCOUNT_SOURCE")
	if accountSource == "api" {
		apiURL := os.Getenv("ACCOUNT_API_URL")
		apiToken := os.Getenv("ACCOUNT_API_TOKEN")
		if apiURL == "" || apiToken == "" {
			return
		}
		loadAccountsFromAPI(apiURL, apiToken, 1)
		if len(RefreshTokens) > nextRefreshTokenIndex {
			tokenMutex.Lock()
			defer tokenMutex.Unlock()
			for attempt := 0; attempt < maxRefreshAttempt; attempt++ {
				newToken, err := GetAccessTokenFromRefreshToken(RefreshTokens[nextRefreshTokenIndex])
				if err == nil {
					RefreshTokens[nextRefreshTokenIndex].AccessToken = newToken
					ActiveTokens = append(ActiveTokens, nextRefreshTokenIndex)
					NormalLogger.Printf("Added new token from API: %d\n", nextRefreshTokenIndex)
					nextRefreshTokenIndex++
					break
				}
			}
		}
	}
}

func updateCSVEnabled(refreshToken string) {
	if csvPath == "" {
		return
	}
	go func() {
		csvMutex.Lock()
		defer csvMutex.Unlock()

		file, err := os.ReadFile(csvPath)
		if err != nil {
			NormalLogger.Printf("Failed to read CSV: %v\n", err)
			return
		}

		lines := strings.Split(string(file), "\n")
		for i := 1; i < len(lines); i++ {
			if strings.Contains(lines[i], refreshToken) {
				parts := strings.Split(lines[i], ",")
				if len(parts) >= 4 && strings.TrimSpace(parts[1]) == refreshToken {
					parts[0] = "False"
					lines[i] = strings.Join(parts, ",")
					break
				}
			}
		}

		_ = os.WriteFile(csvPath, []byte(strings.Join(lines, "\n")), 0644)
	}()
}

func GetBearer() (string, error) {

	initDoOnce.Do(func() {
		activeTokenCountStr := os.Getenv("ACTIVE_TOKEN_COUNT")
		if activeTokenCountStr == "" {
			activeTokenCount = 10
		} else {
			_, _ = fmt.Sscanf(activeTokenCountStr, "%d", &activeTokenCount)
		}

		maxRefreshAttemptStr := os.Getenv("MAX_REFRESH_ATTEMPT")
		if maxRefreshAttemptStr == "" {
			maxRefreshAttempt = 3
		} else {
			_, _ = fmt.Sscanf(maxRefreshAttemptStr, "%d", &maxRefreshAttempt)
		}

		accountSource := os.Getenv("ACCOUNT_SOURCE")
		if accountSource == "" {
			accountSource = "csv"
		}

		if accountSource == "api" {
			apiURL := os.Getenv("ACCOUNT_API_URL")
			apiToken := os.Getenv("ACCOUNT_API_TOKEN")
			if apiURL == "" || apiToken == "" {
				panic("ACCOUNT_API_URL and ACCOUNT_API_TOKEN must be set when ACCOUNT_SOURCE=api")
			}
			NormalLogger.Printf("Loading accounts from API: %s\n", apiURL)
			loadAccountsFromAPI(apiURL, apiToken, activeTokenCount)
		} else {
			csvPath = os.Getenv("ACCOUNTS_CSV_PATH")
			if csvPath == "" {
				panic("ACCOUNTS_CSV_PATH environment variable not set")
			}
			NormalLogger.Printf("Loading accounts from CSV: %s\n", csvPath)
			loadAccountsFromCSV(csvPath)
		}

		if activeTokenCount > len(RefreshTokens) {
			activeTokenCount = len(RefreshTokens)
		}

		tokenMutex.Lock()
		for i := 0; i < activeTokenCount; i++ {
			var accessToken Models.AccessToken
			var err error
			for attempt := 0; attempt < maxRefreshAttempt; attempt++ {
				accessToken, err = GetAccessTokenFromRefreshToken(RefreshTokens[i])
				if err == nil {
					break
				}
				NormalLogger.Printf("Failed to get access token (attempt %d/%d): %v\n", attempt+1, maxRefreshAttempt, err)
			}
			if err != nil {
				continue
			}
			RefreshTokens[i].AccessToken = accessToken
			ActiveTokens = append(ActiveTokens, i)
			NormalLogger.Printf("Get Access Token OK! %s\n", RefreshTokens[i].AccessToken.Token)
		}
		tokenMutex.Unlock()
		nextRefreshTokenIndex = activeTokenCount
	})

	tokenMutex.Lock()
	defer tokenMutex.Unlock()

	now := time.Now().Unix()
	var validIndices []int

	for _, idx := range ActiveTokens {
		if RefreshTokens[idx].AccessToken.ExpiresAt > now {
			validIndices = append(validIndices, idx)
		}
	}

	if len(validIndices) == 0 {
		return "", fmt.Errorf("no valid access tokens available")
	}

	tokenIndex = (tokenIndex + 1) % len(validIndices)
	selectedIdx := validIndices[tokenIndex]
	return RefreshTokens[selectedIdx].AccessToken.Token, nil
}

func StartTokenRefresher() {
	go func() {
		for {
			jitter := time.Duration(rand.Intn(20)+20) * time.Minute
			time.Sleep(jitter)

			tokenMutex.Lock()
			for _, idx := range ActiveTokens {
				newToken, err := GetAccessTokenFromRefreshToken(RefreshTokens[idx])
				if err != nil {
					NormalLogger.Printf("Failed to refresh active token %d: %v\n", idx, err)
					continue
				}
				RefreshTokens[idx].AccessToken = newToken
				NormalLogger.Printf("Refreshed active token %d\n", idx)
			}
			tokenMutex.Unlock()
		}
	}()
}

func GetAccessTokenFromRefreshToken(refreshToken Models.RefreshToken) (Models.AccessToken, error) {
	// Prepare request body
	requestBody := Models.TokenRefreshRequest{
		ClientId:     refreshToken.ClientId,
		ClientSecret: refreshToken.ClientSecret,
		GrantType:    "refresh_token",
		RefreshToken: refreshToken.Token,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return Models.AccessToken{}, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	qUrl := "https://oidc.us-east-1.amazonaws.com/token"
	req, err := http.NewRequest("POST", qUrl, bytes.NewBuffer(jsonData))
	if err != nil {
		return Models.AccessToken{}, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers to match the curl command
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("user-agent", "aws-sdk-rust/1.3.9 os/macos lang/rust/1.87.0")
	req.Header.Set("x-amz-user-agent", "aws-sdk-rust/1.3.9 ua/2.1 api/ssooidc/1.89.0 os/macos lang/rust/1.87.0 m/E app/AmazonQ-For-KIRO_CLI")
	req.Header.Set("amz-sdk-request", "attempt=1; max=3")
	req.Header.Set("amz-sdk-invocation-id", uuid.New().String())
	req.Header.Set("accept", "*/*")
	req.Header.Set("accept-encoding", "gzip")

	// Create HTTP client and make request
	client := &http.Client{
		Transport: getProxyTransport(),
		Timeout:   30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return Models.AccessToken{}, fmt.Errorf("failed to make request: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {

		}
	}(resp.Body)

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Models.AccessToken{}, fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return Models.AccessToken{}, fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var tokenResponse Models.TokenRefreshResponse
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return Models.AccessToken{}, fmt.Errorf("failed to parse response: %w", err)
	}

	// Calculate expiration time
	expiresAt := time.Now().Unix() + int64(tokenResponse.ExpiresIn)

	return Models.AccessToken{
		Token:     tokenResponse.AccessToken,
		ExpiresAt: expiresAt,
	}, nil
}

func CheckAndDisableToken(body []byte, token string) {
	bodyStr := string(body)
	if strings.Contains(bodyStr, "MONTHLY_REQUEST_COUNT") {
		DisableToken(token, "MONTHLY_REQUEST_COUNT")
	} else if strings.Contains(bodyStr, "TEMPORARILY_SUSPENDED") {
		DisableToken(token, "TEMPORARILY_SUSPENDED")
	}
}
