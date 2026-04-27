package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"watchtogether/internal/cache"
	"watchtogether/internal/config"
	"watchtogether/internal/model"
	"watchtogether/internal/store"
)

const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

var (
	ErrInvalidCredentials = errors.New("auth: invalid credentials")
	ErrInvalidToken       = errors.New("auth: invalid token")
)

type Claims struct {
	UserID   string         `json:"uid"`
	Username string         `json:"username"`
	Role     model.UserRole `json:"role"`
	Type     string         `json:"typ"`
	jwt.RegisteredClaims
}

type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type Service struct {
	users    store.UserStore
	sessions cache.SessionCache
	cfg      config.Config
	now      func() time.Time
}

func NewService(users store.UserStore, sessions cache.SessionCache, cfg config.Config) *Service {
	return &Service{users: users, sessions: sessions, cfg: cfg, now: time.Now}
}

func (s *Service) Register(ctx context.Context, username, password, nickname, avatarURL string) (*model.User, TokenPair, error) {
	username = normalizeUsername(username)
	if err := validateCredentials(username, password); err != nil {
		return nil, TokenPair{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, TokenPair{}, err
	}
	user := &model.User{
		Username:     username,
		PasswordHash: hash,
		Nickname:     strings.TrimSpace(nickname),
		AvatarURL:    strings.TrimSpace(avatarURL),
		Role:         model.UserRoleUser,
	}
	if user.Nickname == "" {
		user.Nickname = username
	}
	if err := s.users.Create(ctx, user); err != nil {
		return nil, TokenPair{}, err
	}
	tokens, err := s.issuePair(ctx, user)
	if err != nil {
		return nil, TokenPair{}, err
	}
	return user, tokens, nil
}

func (s *Service) Login(ctx context.Context, username, password string) (*model.User, TokenPair, error) {
	user, err := s.users.GetByUsername(ctx, normalizeUsername(username))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, TokenPair{}, ErrInvalidCredentials
		}
		return nil, TokenPair{}, err
	}
	if !CheckPassword(user.PasswordHash, password) {
		return nil, TokenPair{}, ErrInvalidCredentials
	}
	tokens, err := s.issuePair(ctx, user)
	if err != nil {
		return nil, TokenPair{}, err
	}
	return user, tokens, nil
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	claims, err := s.ParseToken(ctx, refreshToken, TokenTypeRefresh)
	if err != nil {
		return TokenPair{}, err
	}
	stored, err := s.sessions.GetRefreshToken(ctx, claims.UserID)
	if err != nil {
		return TokenPair{}, err
	}
	if stored == "" || stored != refreshToken {
		return TokenPair{}, ErrInvalidToken
	}
	user, err := s.users.GetByID(ctx, claims.UserID)
	if err != nil {
		return TokenPair{}, err
	}
	return s.issuePair(ctx, user)
}

func (s *Service) Logout(ctx context.Context, accessToken, refreshToken string) error {
	for _, tokenString := range []string{accessToken, refreshToken} {
		claims, err := s.ParseToken(ctx, tokenString, "")
		if err != nil {
			continue
		}
		ttl := time.Until(claims.ExpiresAt.Time)
		if ttl < 0 {
			ttl = 0
		}
		if err := s.sessions.BlacklistToken(ctx, claims.ID, ttl); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ParseToken(ctx context.Context, tokenString, expectedType string) (*Claims, error) {
	tokenString = strings.TrimSpace(strings.TrimPrefix(tokenString, "Bearer "))
	if tokenString == "" {
		return nil, ErrInvalidToken
	}
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", token.Header["alg"])
		}
		return []byte(s.cfg.JWTSecret), nil
	}, jwt.WithExpirationRequired(), jwt.WithLeeway(5*time.Second))
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	if expectedType != "" && claims.Type != expectedType {
		return nil, ErrInvalidToken
	}
	if claims.UserID == "" || claims.ID == "" {
		return nil, ErrInvalidToken
	}
	blocked, err := s.sessions.IsBlacklisted(ctx, claims.ID)
	if err != nil {
		return nil, err
	}
	if blocked {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

func ExtractBearer(header string) string {
	const prefix = "Bearer "
	if strings.HasPrefix(header, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(header, prefix))
	}
	return ""
}

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (s *Service) issuePair(ctx context.Context, user *model.User) (TokenPair, error) {
	access, accessExp, err := s.sign(user, TokenTypeAccess, s.cfg.JWTAccessTTL)
	if err != nil {
		return TokenPair{}, err
	}
	refresh, _, err := s.sign(user, TokenTypeRefresh, s.cfg.JWTRefreshTTL)
	if err != nil {
		return TokenPair{}, err
	}
	if err := s.sessions.SetRefreshToken(ctx, user.ID, refresh, s.cfg.JWTRefreshTTL); err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresAt:    accessExp,
	}, nil
}

func (s *Service) sign(user *model.User, tokenType string, ttl time.Duration) (string, time.Time, error) {
	now := s.now().UTC()
	expiresAt := now.Add(ttl)
	jti := uuid.NewString()
	claims := Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		Type:     tokenType,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			ID:        jti,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWTSecret))
	return token, expiresAt, err
}

func normalizeUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func validateCredentials(username, password string) error {
	if len(username) < 3 || len(username) > 40 {
		return ErrInvalidCredentials
	}
	if len(password) < 8 || len(password) > 128 {
		return ErrInvalidCredentials
	}
	return nil
}
