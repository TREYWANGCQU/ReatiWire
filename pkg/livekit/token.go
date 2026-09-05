// collaboration_tool_solution/team_collab/pkg/livekit/token.go
package livekit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// VideoGrant LiveKit 权限声明
type VideoGrant struct {
	RoomJoin     bool   `json:"roomJoin"`
	Room         string `json:"room"`
	CanPublish   bool   `json:"canPublish"`
	CanSubscribe bool   `json:"canSubscribe"`
}

// JWTPayload LiveKit 鉴权载荷
type JWTPayload struct {
	Iss      string      `json:"iss"`
	Sub      string      `json:"sub"`
	Iat      int64       `json:"iat"`
	Exp      int64       `json:"exp"`
	Nbf      int64       `json:"nbf"`
	Name     string      `json:"name"`
	Video    *VideoGrant `json:"video"`
	Metadata string      `json:"metadata"`
}

// TokenGenerator LiveKit 入会凭据签发器
type TokenGenerator struct {
	apiKey    string
	apiSecret string
}

// NewTokenGenerator 初始化 Token 签发器
func NewTokenGenerator(apiKey, apiSecret string) *TokenGenerator {
	if apiKey == "" {
		apiKey = "reati_wire_api_key"
	}
	if apiSecret == "" {
		apiSecret = "reati_wire_api_secret_32bytes_min"
	}
	return &TokenGenerator{
		apiKey:    apiKey,
		apiSecret: apiSecret,
	}
}

// CreateJoinToken 签发进入指定会议室的 JWT 凭证
func (g *TokenGenerator) CreateJoinToken(roomName, identity, displayName string, validDuration time.Duration) (string, error) {
	if validDuration <= 0 {
		validDuration = 6 * time.Hour
	}
	now := time.Now().Unix()

	header := map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	}
	headerJSON, _ := json.Marshal(header)
	encodedHeader := base64.RawURLEncoding.EncodeToString(headerJSON)

	payload := JWTPayload{
		Iss:  g.apiKey,
		Sub:  identity,
		Iat:  now,
		Nbf:  now - 5,
		Exp:  now + int64(validDuration.Seconds()),
		Name: displayName,
		Video: &VideoGrant{
			RoomJoin:     true,
			Room:         roomName,
			CanPublish:   true,
			CanSubscribe: true,
		},
		Metadata: `{"dynacast":true,"simulcast":true,"max_bandwidth":"85mbps"}`,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal jwt payload: %w", err)
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payloadJSON)

	sigInput := encodedHeader + "." + encodedPayload
	mac := hmac.New(sha256.New, []byte(g.apiSecret))
	mac.Write([]byte(sigInput))
	signature := mac.Sum(nil)
	encodedSig := base64.RawURLEncoding.EncodeToString(signature)

	return sigInput + "." + encodedSig, nil
}
