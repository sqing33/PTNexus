package handler

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/pt-nexus/server-go/internal/service"
)

type AuthHandler struct {
	auth *service.AuthService
}

func NewAuthHandler(auth *service.AuthService) *AuthHandler {
	return &AuthHandler{auth: auth}
}

func (h *AuthHandler) Login(c *gin.Context) {
	payload := struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}{}
	_ = c.ShouldBindJSON(&payload)

	response, status := h.auth.Login(strings.TrimSpace(payload.Username), payload.Password)
	c.JSON(status, response)
}

func (h *AuthHandler) Status(c *gin.Context) {
	c.JSON(200, h.auth.Status())
}

func (h *AuthHandler) ChangePassword(c *gin.Context) {
	payload := struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		OldPassword string `json:"old_password"`
	}{}
	_ = c.ShouldBindJSON(&payload)

	response, status := h.auth.ChangePassword(payload.OldPassword, strings.TrimSpace(payload.Username), payload.Password)
	c.JSON(status, response)
}
