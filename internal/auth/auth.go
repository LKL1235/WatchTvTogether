package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"watchtogether/internal/cache"
	"watchtogether/internal/config"
	verifyemail "watchtogether/internal/email"
	"watchtogether/internal/emailcode"
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
	ErrInvalidEmail       = errors.New("auth: invalid email")
	ErrWeakPassword       = errors.New("auth: weak password")
	ErrInvalidUsername    = errors.New("auth: invalid username")
)

var usernameCharset = regexp.MustCompile(`^[a-z0-9_]{3,40}$`)

type Service struct {
	users    store.UserStore
	sessions cache.SessionCache
	cfg      config.Config
	codes    *emailcode.Store
	now      func() time.Time

	afterLogin func(context.Context) error
}

func NewService(users store.UserStore, sessions cache.SessionCache, codes *emailcode.Store, cfg config.Config) *Service {
	return &Service{users: users, sessions: sessions, codes: codes, cfg: cfg, now: time.Now}
}

// SetAfterLogin registers a hook invoked after successful login and register token issuance (e.g. periodic cleanup).
func (s *Service) SetAfterLogin(fn func(context.Context) error) {
	s.afterLogin = fn
}

func (s *Service) RegisterWithCode(ctx context.Context, emailAddr, username, password, code, nickname, avatarURL string) (*model.User, TokenPair, error) {
	emailNorm, err := normalizeEmail(emailAddr)
	if err != nil {
		return nil, TokenPair{}, err
	}
	usernameNorm := normalizeUsername(username)
	if err := validateUsername(usernameNorm); err != nil {
		return nil, TokenPair{}, err
	}
	if err := validatePassword(password); err != nil {
		return nil, TokenPair{}, err
	}
	if s.codes == nil {
		return nil, TokenPair{}, errors.New("auth: verification store not configured")
	}
	if err := s.codes.Verify(ctx, emailNorm, verifyemail.PurposeRegister, code, s.cfg.EmailCodeMaxAttempts, s.now()); err != nil {
		return nil, TokenPair{}, err
	}
	if _, err := s.users.GetByEmail(ctx, emailNorm); err == nil {
		return nil, TokenPair{}, store.ErrConflict
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, TokenPair{}, err
	}
	if _, err := s.users.GetByUsername(ctx, usernameNorm); err == nil {
		return nil, TokenPair{}, store.ErrConflict
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, TokenPair{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return nil, TokenPair{}, err
	}
	nick := strings.TrimSpace(nickname)
	if nick == "" {
		nick = usernameNorm
	}
	user := &model.User{
		Email:        emailNorm,
		Username:     usernameNorm,
		PasswordHash: hash,
		Nickname:     nick,
		AvatarURL:    strings.TrimSpace(avatarURL),
		Role:         model.UserRoleUser,
	}
	if err := s.users.Create(ctx, user); err != nil {
		return nil, TokenPair{}, err
	}
	tokens, err := s.issuePair(ctx, user)
	if err != nil {
		return nil, TokenPair{}, err
	}
	s.runAfterLogin(ctx)
	return user, tokens, nil
}

func (s *Service) Login(ctx context.Context, login, password string) (*model.User, TokenPair, error) {
	login = strings.TrimSpace(login)
	if login == "" {
		return nil, TokenPair{}, ErrInvalidCredentials
	}
	var user *model.User
	var err error
	if strings.Contains(login, "@") {
		emailNorm, e := normalizeEmail(login)
		if e != nil {
			return nil, TokenPair{}, ErrInvalidCredentials
		}
		user, err = s.users.GetByEmail(ctx, emailNorm)
	} else {
		user, err = s.users.GetByUsername(ctx, normalizeUsername(login))
	}
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
	s.runAfterLogin(ctx)
	return user, tokens, nil
}

func (s *Service) ResetPassword(ctx context.Context, emailAddr, code, newPassword string) error {
	emailNorm, err := normalizeEmail(emailAddr)
	if err != nil {
		return err
	}
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	if s.codes == nil {
		return errors.New("auth: verification store not configured")
	}
	if err := s.codes.Verify(ctx, emailNorm, verifyemail.PurposeResetPassword, code, s.cfg.EmailCodeMaxAttempts, s.now()); err != nil {
		return err
	}
	user, err := s.users.GetByEmail(ctx, emailNorm)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrInvalidCredentials
		}
		return err
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	user.PasswordHash = hash
	if err := s.users.Update(ctx, user); err != nil {
		return err
	}
	return s.sessions.DeleteRefreshToken(ctx, user.ID)
}

func (s *Service) runAfterLogin(ctx context.Context) {
	if s.afterLogin == nil {
		return
	}
	_ = s.afterLogin(ctx)
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
	tokens, err := s.issuePair(ctx, user)
	if err != nil {
		return TokenPair{}, err
	}
	s.runAfterLogin(ctx)
	return tokens, nil
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

func validateUsername(username string) error {
	if !usernameCharset.MatchString(username) {
		return ErrInvalidUsername
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < 8 || len(password) > 128 {
		return ErrWeakPassword
	}
	var lower, digit bool
	for _, r := range password {
		if unicode.IsLower(r) {
			lower = true
		}
		if unicode.IsDigit(r) {
			digit = true
		}
	}
	if !lower || !digit {
		return ErrWeakPassword
	}
	return nil
}

// NormalizeEmailForRequest trims, lowercases, and validates an email address for API handlers.
func NormalizeEmailForRequest(raw string) (string, error) {
	return normalizeEmail(raw)
}

func normalizeEmail(raw string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return "", ErrInvalidEmail
	}
	addr, err := mail.ParseAddress(s)
	if err != nil {
		wrapped := fmt.Sprintf("<%s>", s)
		addr, err = mail.ParseAddress(wrapped)
		if err != nil {
			return "", ErrInvalidEmail
		}
	}
	at := strings.LastIndex(addr.Address, "@")
	if at <= 0 || at == len(addr.Address)-1 {
		return "", ErrInvalidEmail
	}
	local := addr.Address[:at]
	domain := addr.Address[at+1:]
	if local == "" || domain == "" {
		return "", ErrInvalidEmail
	}
	return local + "@" + domain, nil
}

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
