package ably

import (
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"watchtogether/internal/config"
)

// IssueRoomJWT returns an Ably-compatible JWT for realtime subscribe/presence/history on the room control channel.
func IssueRoomJWT(cfg config.Config, roomID, clientID string, now time.Time) (token string, expiresAt time.Time, err error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return "", time.Time{}, errors.New("client id is required")
	}
	if cfg.AblyKeyName == "" || cfg.AblyKeySecret == "" {
		return "", time.Time{}, ErrRootKeyRequired
	}
	channel := ChannelName(cfg.AblyChannelPrefix, roomID)
	capJSON, err := RoomCapability(channel)
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt = now.UTC().Add(cfg.AblyJWTTTL)
	claims := jwt.MapClaims{
		"iat":               now.UTC().Unix(),
		"exp":               expiresAt.Unix(),
		"x-ably-capability": capJSON,
		"x-ably-clientId":   clientID,
		"x-ably-token-type": "jwt",
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tok.Header["typ"] = "JWT"
	tok.Header["kid"] = cfg.AblyKeyName
	signed, err := tok.SignedString([]byte(cfg.AblyKeySecret))
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}
