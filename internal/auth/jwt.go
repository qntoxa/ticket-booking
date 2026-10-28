package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/user/ticket-booking/internal/domain"
)

var (
	ErrInvalidToken = errors.New("invalid or malformed token")
	ErrExpiredToken = errors.New("token has expired")
)

type TokenType string

const (
	TokenAccess  TokenType = "access"
	TokenRefresh TokenType = "refresh"
)

type Claims struct {
	UserID    string      `json:"sub"`
	Email     string      `json:"email"`
	Role      domain.Role `json:"role"`
	Type      TokenType   `json:"typ"`
	IssuedAt  int64       `json:"iat"`
	ExpiresAt int64       `json:"exp"`
}

type Manager struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewManager(secret string, accessTTL, refreshTTL time.Duration) *Manager {
	return &Manager{
		secret:     []byte(secret),
		accessTTL:  accessTTL,
		refreshTTL: refreshTTL,
	}
}

func (m *Manager) IssuePair(user domain.User) (accessToken, refreshToken string, err error) {
	now := time.Now().UTC()
	accessToken, err = m.sign(Claims{
		UserID:    user.ID,
		Email:     user.Email,
		Role:      user.Role,
		Type:      TokenAccess,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(m.accessTTL).Unix(),
	})
	if err != nil {
		return "", "", err
	}

	refreshToken, err = m.sign(Claims{
		UserID:    user.ID,
		Email:     user.Email,
		Role:      user.Role,
		Type:      TokenRefresh,
		IssuedAt:  now.Unix(),
		ExpiresAt: now.Add(m.refreshTTL).Unix(),
	})
	return accessToken, refreshToken, err
}

func (m *Manager) Verify(token string, expectedType TokenType) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, ErrInvalidToken
	}

	unsigned := parts[0] + "." + parts[1]
	expectedSig := m.computeHMAC(unsigned)
	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return Claims{}, ErrInvalidToken
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}

	var claims Claims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return Claims{}, ErrInvalidToken
	}

	if claims.Type != expectedType {
		return Claims{}, ErrInvalidToken
	}

	if time.Now().UTC().Unix() > claims.ExpiresAt {
		return Claims{}, ErrExpiredToken
	}

	return claims, nil
}

func (m *Manager) sign(claims Claims) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	unsigned := header + "." + payload
	sig := m.computeHMAC(unsigned)
	return unsigned + "." + sig, nil
}

func (m *Manager) computeHMAC(unsigned string) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(unsigned))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func HashPassword(password string) string {
	sum := sha256.Sum256([]byte("salt:ticket-booking:" + password))
	return hex.EncodeToString(sum[:])
}
