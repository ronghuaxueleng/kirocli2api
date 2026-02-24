package API

import (
	"kilocli2api/Models"
	"kilocli2api/Utils"
	"net/http"

	"github.com/gin-gonic/gin"
)

func DebugToken(c *gin.Context) {
	var rt Models.RefreshToken
	if err := c.ShouldBindJSON(&rt); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	if rt.Token == "" || rt.ClientId == "" || rt.ClientSecret == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "refresh_token, client_id, and client_secret are required"})
		return
	}

	accessToken, err := Utils.GetAccessTokenFromRefreshToken(rt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"access_token": accessToken.Token, "expires_at": accessToken.ExpiresAt})
}
