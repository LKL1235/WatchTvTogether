package emailcode

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

var (
	deterministicTestCode   string
	deterministicTestCodeMu sync.Mutex
)

// UseDeterministicEmailCodeForTests makes GenerateNumericCode return the given string (length must match n). For integration tests only.
func UseDeterministicEmailCodeForTests(code string) {
	deterministicTestCodeMu.Lock()
	deterministicTestCode = code
	deterministicTestCodeMu.Unlock()
}

func ClearDeterministicEmailCodeForTest() {
	deterministicTestCodeMu.Lock()
	deterministicTestCode = ""
	deterministicTestCodeMu.Unlock()
}

var (
	ErrCooldown    = errors.New("emailcode: cooldown")
	ErrDailyLimit  = errors.New("emailcode: daily limit")
	ErrInvalidCode = errors.New("emailcode: invalid code")
	ErrExpired     = errors.New("emailcode: expired")
	ErrMaxAttempts = errors.New("emailcode: too many attempts")
)

type CooldownError struct {
	RetryAfter time.Duration
}

func (e *CooldownError) Error() string {
	return "emailcode: cooldown"
}

func (e *CooldownError) Unwrap() error { return ErrCooldown }

// Store persists hashed verification codes and send limits (Redis or in-memory for tests).
type Store struct {
	redis goredis.UniversalClient
	mu    sync.Mutex
	mem   map[string]*memEntry // key = purpose + "|" + email
}

type memEntry struct {
	hash       string
	expiresAt  time.Time
	attempts   int
	coolUntil  time.Time
	dailyDay   string
	dailyCount int
}

func NewStore(redis goredis.UniversalClient) *Store {
	return &Store{redis: redis, mem: make(map[string]*memEntry)}
}

func (s *Store) keyCode(purpose, email string) string {
	return fmt.Sprintf("email:code:v1:%s:%s", purpose, strings.ToLower(strings.TrimSpace(email)))
}

func (s *Store) keyCooldown(purpose, email string) string {
	return fmt.Sprintf("email:cool:v1:%s:%s", purpose, strings.ToLower(strings.TrimSpace(email)))
}

func (s *Store) keyDaily(purpose, email, day string) string {
	return fmt.Sprintf("email:daily:v1:%s:%s:%s", purpose, strings.ToLower(strings.TrimSpace(email)), day)
}

func utcDay(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

func secondsUntilUTCEndOfDay(now time.Time) time.Duration {
	utc := now.UTC()
	next := time.Date(utc.Year(), utc.Month(), utc.Day()+1, 0, 0, 0, 0, time.UTC)
	return next.Sub(utc)
}

// PrecheckSend verifies cooldown and reserves one daily send slot (call RollbackPrecheck if email send fails).
func (s *Store) PrecheckSend(ctx context.Context, email, purpose string, cooldown time.Duration, dailyLimit int, now time.Time) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || purpose == "" {
		return errors.New("emailcode: empty email or purpose")
	}
	if s.redis != nil {
		return s.precheckSendRedis(ctx, email, purpose, cooldown, dailyLimit, now)
	}
	return s.precheckSendMem(email, purpose, cooldown, dailyLimit, now)
}

func (s *Store) precheckSendRedis(ctx context.Context, email, purpose string, cooldown time.Duration, dailyLimit int, now time.Time) error {
	coolKey := s.keyCooldown(purpose, email)
	ttl, err := s.redis.TTL(ctx, coolKey).Result()
	if err != nil {
		return err
	}
	if ttl > 0 {
		return &CooldownError{RetryAfter: ttl}
	}
	day := utcDay(now)
	dailyKey := s.keyDaily(purpose, email, day)
	pipe := s.redis.TxPipeline()
	incr := pipe.Incr(ctx, dailyKey)
	pipe.Expire(ctx, dailyKey, secondsUntilUTCEndOfDay(now))
	_, err = pipe.Exec(ctx)
	if err != nil {
		return err
	}
	n, err := incr.Result()
	if err != nil {
		return err
	}
	if int(n) > dailyLimit {
		_ = s.redis.Decr(ctx, dailyKey).Err()
		return ErrDailyLimit
	}
	return nil
}

func (s *Store) RollbackPrecheck(ctx context.Context, email, purpose string, now time.Time) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if s.redis != nil {
		day := utcDay(now)
		return s.redis.Decr(ctx, s.keyDaily(purpose, email, day)).Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := purpose + "|" + email
	e := s.mem[k]
	if e == nil {
		return nil
	}
	if e.dailyCount > 0 {
		e.dailyCount--
	}
	return nil
}

// CommitSend stores the bcrypt hash of the code and starts cooldown (after successful outbound email).
func (s *Store) CommitSend(ctx context.Context, email, purpose, plainCode string, codeHashTTL, cooldown time.Duration, now time.Time) error {
	email = strings.ToLower(strings.TrimSpace(email))
	hash, err := bcrypt.GenerateFromPassword([]byte(plainCode), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if s.redis != nil {
		return s.commitSendRedis(ctx, email, purpose, string(hash), codeHashTTL, cooldown, now)
	}
	return s.commitSendMem(email, purpose, string(hash), codeHashTTL, cooldown, now)
}

func (s *Store) commitSendRedis(ctx context.Context, email, purpose, codeHash string, codeHashTTL, cooldown time.Duration, now time.Time) error {
	expiresAt := now.Add(codeHashTTL).Unix()
	codeKey := s.keyCode(purpose, email)
	if err := s.redis.HSet(ctx, codeKey, "h", codeHash, "exp", expiresAt, "n", 0).Err(); err != nil {
		return err
	}
	if err := s.redis.Expire(ctx, codeKey, codeHashTTL).Err(); err != nil {
		return err
	}
	return s.redis.Set(ctx, s.keyCooldown(purpose, email), "1", cooldown).Err()
}

func (s *Store) precheckSendMem(email, purpose string, cooldown time.Duration, dailyLimit int, now time.Time) error {
	k := purpose + "|" + email
	day := utcDay(now)
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.mem[k]
	if !ok {
		e = &memEntry{}
		s.mem[k] = e
	}
	if now.Before(e.coolUntil) {
		return &CooldownError{RetryAfter: e.coolUntil.Sub(now)}
	}
	if e.dailyDay != day {
		e.dailyDay = day
		e.dailyCount = 0
	}
	if e.dailyCount >= dailyLimit {
		return ErrDailyLimit
	}
	e.dailyCount++
	return nil
}

func (s *Store) commitSendMem(email, purpose, codeHash string, codeHashTTL, cooldown time.Duration, now time.Time) error {
	k := purpose + "|" + email
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.mem[k]
	if e == nil {
		e = &memEntry{}
		s.mem[k] = e
	}
	e.hash = codeHash
	e.expiresAt = now.Add(codeHashTTL)
	e.attempts = 0
	e.coolUntil = now.Add(cooldown)
	return nil
}

// Verify compares plaintext code with stored hash and deletes the record on success.
func (s *Store) Verify(ctx context.Context, email, purpose, plain string, maxAttempts int, now time.Time) error {
	email = strings.ToLower(strings.TrimSpace(email))
	if s.redis != nil {
		return s.verifyRedis(ctx, email, purpose, plain, maxAttempts, now)
	}
	return s.verifyMem(email, purpose, plain, maxAttempts, now)
}

func (s *Store) verifyRedis(ctx context.Context, email, purpose, plain string, maxAttempts int, now time.Time) error {
	key := s.keyCode(purpose, email)
	h, err := s.redis.HGetAll(ctx, key).Result()
	if err != nil {
		return err
	}
	if len(h) == 0 {
		return ErrInvalidCode
	}
	expUnix, _ := strconv.ParseInt(h["exp"], 10, 64)
	if now.Unix() > expUnix {
		_ = s.redis.Del(ctx, key).Err()
		return ErrExpired
	}
	if err := bcrypt.CompareHashAndPassword([]byte(h["h"]), []byte(strings.TrimSpace(plain))); err != nil {
		n, _ := strconv.Atoi(h["n"])
		n++
		_ = s.redis.HSet(ctx, key, "n", n).Err()
		if n >= maxAttempts {
			_ = s.redis.Del(ctx, key).Err()
			return ErrMaxAttempts
		}
		return ErrInvalidCode
	}
	_ = s.redis.Del(ctx, key).Err()
	return nil
}

func (s *Store) verifyMem(email, purpose, plain string, maxAttempts int, now time.Time) error {
	k := purpose + "|" + email
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.mem[k]
	if !ok || e.hash == "" {
		return ErrInvalidCode
	}
	if now.After(e.expiresAt) {
		e.hash = ""
		return ErrExpired
	}
	if err := bcrypt.CompareHashAndPassword([]byte(e.hash), []byte(strings.TrimSpace(plain))); err != nil {
		e.attempts++
		if e.attempts >= maxAttempts {
			e.hash = ""
			return ErrMaxAttempts
		}
		return ErrInvalidCode
	}
	e.hash = ""
	return nil
}

// GenerateNumericCode returns an n-digit numeric string (leading zeros allowed).
func GenerateNumericCode(n int) (string, error) {
	deterministicTestCodeMu.Lock()
	dc := deterministicTestCode
	deterministicTestCodeMu.Unlock()
	if dc != "" {
		if len(dc) != n {
			return "", errors.New("emailcode: deterministic code length mismatch")
		}
		return dc, nil
	}
	if n <= 0 || n > 12 {
		return "", errors.New("emailcode: invalid length")
	}
	const digits = "0123456789"
	buf := make([]byte, n)
	for i := range buf {
		var b [1]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		buf[i] = digits[int(b[0])%10]
	}
	return string(buf), nil
}

// HashForAudit returns a non-reversible digest of the code for logs (never log plaintext).
func HashForAudit(code string) string {
	sum := sha256.Sum256([]byte(code))
	return fmt.Sprintf("%x", sum[:8])
}
