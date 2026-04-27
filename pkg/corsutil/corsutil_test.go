package corsutil

import (
	"net/http/httptest"
	"testing"
)

func TestGinConfigAllowAll(t *testing.T) {
	cfg := GinConfig(nil)
	if !cfg.AllowAllOrigins {
		t.Fatalf("expected AllowAllOrigins for nil list")
	}
	cfg = GinConfig([]string{" * "})
	if !cfg.AllowAllOrigins {
		t.Fatalf("expected AllowAllOrigins for *")
	}
}

func TestCheckOriginList(t *testing.T) {
	chk := CheckOrigin([]string{"https://app.example.com", "http://localhost:5173"})
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	if !chk(req) {
		t.Fatal("expected app.example.com allowed")
	}
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("Origin", "https://evil.com")
	if chk(req2) {
		t.Fatal("expected evil.com denied")
	}
}
