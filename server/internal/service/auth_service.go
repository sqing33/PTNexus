package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pt-nexus/server-go/internal/config"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	cfg *config.Manager
}

func NewAuthService(cfg *config.Manager) *AuthService {
	return &AuthService{cfg: cfg}
}

func (s *AuthService) Login(username, password string) (map[string]any, int) {
	confUser, confHash, confPlain, mustChangePassword := s.currentCredentials()
	if username != confUser {
		return map[string]any{"success": false, "message": "用户名或密码错误"}, 401
	}

	if confHash != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(confHash), []byte(password)); err != nil {
			return map[string]any{"success": false, "message": "用户名或密码错误"}, 401
		}
	} else if confPlain == "" || confPlain != password {
		return map[string]any{"success": false, "message": "用户名或密码错误"}, 401
	}

	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"sub": username,
		"iat": now.Unix(),
		"exp": now.Add(7 * 24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(s.jwtSecret()))
	if err != nil {
		return map[string]any{"success": false, "message": "签发令牌失败"}, 500
	}

	isTempPassword := confHash == "" && confPlain != ""
	return map[string]any{
		"success":              true,
		"token":                signed,
		"is_temp_password":     isTempPassword,
		"must_change_password": isTempPassword || mustChangePassword,
	}, 200
}

func (s *AuthService) Status() map[string]any {
	confUser, _, _, mustChangePassword := s.currentCredentials()
	return map[string]any{
		"success":              true,
		"username":             confUser,
		"must_change_password": mustChangePassword,
	}
}

func (s *AuthService) ChangePassword(oldPassword, newUsername, newPassword string) (map[string]any, int) {
	if len(newPassword) < 6 {
		return map[string]any{"success": false, "message": "密码至少 6 位"}, 400
	}
	if newUsername == "" {
		newUsername = "admin"
	}

	_, confHash, confPlain, _ := s.currentCredentials()
	if confHash != "" {
		if oldPassword == "" || bcrypt.CompareHashAndPassword([]byte(confHash), []byte(oldPassword)) != nil {
			return map[string]any{"success": false, "message": "当前密码不正确"}, 401
		}
	} else {
		if confPlain == "" || oldPassword != confPlain {
			return map[string]any{"success": false, "message": "当前密码不正确"}, 401
		}
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return map[string]any{"success": false, "message": "密码哈希失败"}, 500
	}

	cfg := s.cfg.Get()
	authMap := authAsMap(cfg["auth"])
	authMap["username"] = newUsername
	authMap["password_hash"] = string(hashed)
	authMap["must_change_password"] = false
	cfg["auth"] = authMap

	if err := s.cfg.Save(cfg); err != nil {
		return map[string]any{"success": false, "message": "保存配置失败"}, 500
	}

	_ = os.Unsetenv("AUTH_PASSWORD")
	_ = os.Unsetenv("AUTH_PASSWORD_HASH")

	return map[string]any{"success": true}, 200
}

func (s *AuthService) ValidateJWT(token string) (jwt.MapClaims, error) {
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(s.jwtSecret()), nil
	})
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, errors.New("invalid token")
	}

	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid claims")
	}
	return claims, nil
}

func (s *AuthService) IsUserStillValid(subject string) bool {
	confUser, confHash, confPlain, _ := s.currentCredentials()
	if subject == "" || confUser != subject {
		return false
	}
	return confHash != "" || confPlain != "" || os.Getenv("AUTH_PASSWORD_HASH") != ""
}

func (s *AuthService) jwtSecret() string {
	if secret := os.Getenv("JWT_SECRET"); secret != "" {
		return secret
	}
	username, passwordHash, passwordPlain, _ := s.currentCredentials()
	input := fmt.Sprintf("%s:%s", username, firstNonEmpty(passwordHash, passwordPlain))
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

func (s *AuthService) currentCredentials() (username, passwordHash, passwordPlain string, mustChangePassword bool) {
	cfg := s.cfg.Get()
	authMap := authAsMap(cfg["auth"])

	username = authToString(authMap["username"], firstNonEmpty(os.Getenv("AUTH_USERNAME"), "admin"))
	passwordHash = firstNonEmpty(authToString(authMap["password_hash"], ""), os.Getenv("AUTH_PASSWORD_HASH"))
	if passwordHash == "" {
		passwordPlain = os.Getenv("AUTH_PASSWORD")
	}
	mustChangePassword = authToBool(authMap["must_change_password"], true)
	return
}

func authAsMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func authToString(value any, fallback string) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return fallback
}

func authToBool(value any, fallback bool) bool {
	if typed, ok := value.(bool); ok {
		return typed
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
