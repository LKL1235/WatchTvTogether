package ably_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"watchtogether/internal/config"
	ablyrealtime "watchtogether/internal/realtime/ably"
)

func TestIssueRoomJWTClaimsAndHeader(t *testing.T) {
	cfg := config.Default()
	cfg.AblyKeyName = "mykey.name"
	cfg.AblyKeySecret = "mysecretvalue"
	cfg.AblyChannelPrefix = "watchtogether"
	cfg.AblyJWTTTL = 15 * time.Minute
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	token, exp, err := ablyrealtime.IssueRoomJWT(cfg, "room-99", "user-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(token, ".") {
		t.Fatalf("expected jwt: %s", token)
	}
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		return []byte(cfg.AblyKeySecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithoutClaimsValidation())
	if err != nil || !parsed.Valid {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Header["kid"] != "mykey.name" || parsed.Header["alg"] != "HS256" {
		t.Fatalf("header: %#v", parsed.Header)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatal("map claims")
	}
	capStr, _ := claims["x-ably-capability"].(string)
	var cap map[string][]string
	if err := json.Unmarshal([]byte(capStr), &cap); err != nil {
		t.Fatal(err)
	}
	ch := "watchtogether:room:room-99:control"
	ops := cap[ch]
	if len(ops) != 3 {
		t.Fatalf("ops = %#v", ops)
	}
	for _, forbidden := range []string{"publish", "*"} {
		for _, op := range ops {
			if op == forbidden {
				t.Fatalf("capability should not include %q", forbidden)
			}
		}
	}
	if claims["x-ably-clientId"] != "user-1" {
		t.Fatalf("clientId = %v", claims["x-ably-clientId"])
	}
	if !exp.Equal(now.Add(15 * time.Minute)) {
		t.Fatalf("expiresAt = %v want %v", exp, now.Add(15*time.Minute))
	}
}
