package usecase

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type tokenClaims struct {
	Subject string   `json:"sub"`
	Roles   []string `json:"roles"`
	Expires int64    `json:"exp"`
	Issued  int64    `json:"iat"`
	TokenID string   `json:"jti"`
}

type jwtHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
	KeyID     string `json:"kid"`
}

func signAccessToken(claims tokenClaims, activeKeyID string, keys map[string][]byte) (string, error) {
	key, ok := keys[activeKeyID]
	if !ok || len(key) < 32 {
		return "", errors.New("active JWT signing key is missing or shorter than 32 bytes")
	}
	header, err := json.Marshal(jwtHeader{Algorithm: "HS256", Type: "JWT", KeyID: activeKeyID})
	if err != nil {
		return "", fmt.Errorf("marshal JWT header: %w", err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal JWT claims: %w", err)
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(unsigned))
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func verifyAccessToken(token string, keys map[string][]byte, now time.Time) (*tokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed JWT")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("malformed JWT header")
	}
	var header jwtHeader
	if err := json.Unmarshal(headerBytes, &header); err != nil || header.Algorithm != "HS256" || header.Type != "JWT" {
		return nil, errors.New("unsupported JWT header")
	}
	key, ok := keys[header.KeyID]
	if !ok || len(key) < 32 {
		return nil, errors.New("unknown JWT key id")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, errors.New("malformed JWT signature")
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return nil, errors.New("invalid JWT signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("malformed JWT claims")
	}
	var claims tokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, errors.New("malformed JWT claims")
	}
	if claims.Subject == "" || claims.TokenID == "" || claims.Expires <= now.Unix() || claims.Issued > now.Add(time.Minute).Unix() {
		return nil, errors.New("expired or invalid JWT claims")
	}
	return &claims, nil
}

func newUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate token id: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
