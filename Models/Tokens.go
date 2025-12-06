package Models

type AccessToken struct {
	Token     string
	ExpiresAt int64
}

type RefreshToken struct {
	ID           int
	Token        string
	ClientId     string
	ClientSecret string
	AccessToken  AccessToken
}

// TokenRefreshRequest is the request body for refreshing access token
type TokenRefreshRequest struct {
	ClientId     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	GrantType    string `json:"grantType"`
	RefreshToken string `json:"refreshToken"`
}

// TokenRefreshResponse is the response from token refresh endpoint
type TokenRefreshResponse struct {
	AccessToken        string  `json:"accessToken"`
	AwsSsoAppSessionId *string `json:"aws_sso_app_session_id"`
	ExpiresIn          int     `json:"expiresIn"`
	IdToken            *string `json:"idToken"`
	IssuedTokenType    *string `json:"issuedTokenType"`
	OriginSessionId    *string `json:"originSessionId"`
	RefreshToken       string  `json:"refreshToken"`
	TokenType          string  `json:"tokenType"`
}
