package email

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/resend/resend-go/v3"

	"watchtogether/internal/config"
)

const (
	PurposeRegister      = "register"
	PurposeResetPassword = "reset_password"
)

// Sender sends transactional email via Resend when configured.
type Sender struct {
	cfg    config.Config
	client *resend.Client
}

func NewSender(cfg config.Config) *Sender {
	key := strings.TrimSpace(cfg.ResendAPIKey)
	if key == "" {
		return &Sender{cfg: cfg}
	}
	return &Sender{cfg: cfg, client: resend.NewClient(key)}
}

func (s *Sender) Enabled() bool {
	return s != nil && s.client != nil
}

// SendVerificationCode sends a 6-digit code email. Returns Resend email id when sent.
func (s *Sender) SendVerificationCode(ctx context.Context, to, purpose, code string) (emailID string, err error) {
	if !s.Enabled() {
		log.Printf("email: RESEND_API_KEY not set; skipping send to %s (purpose=%s)", redactEmail(to), purpose)
		return "", ErrDisabled
	}
	subject, html, text := buildVerificationEmail(purpose, code, s.cfg.EmailCodeTTL)
	resp, err := s.client.Emails.SendWithContext(ctx, &resend.SendEmailRequest{
		From:    s.cfg.EmailFrom,
		To:      []string{to},
		Subject: subject,
		Html:    html,
		Text:    text,
	})
	if err != nil {
		return "", err
	}
	return resp.Id, nil
}

func redactEmail(e string) string {
	e = strings.TrimSpace(e)
	if e == "" {
		return "(empty)"
	}
	at := strings.Index(e, "@")
	if at <= 0 {
		return "***"
	}
	return e[:1] + "***@" + e[at+1:]
}

func buildVerificationEmail(purpose, code string, ttl time.Duration) (subject, html, text string) {
	var purposeLabel string
	switch purpose {
	case PurposeRegister:
		purposeLabel = "注册"
	case PurposeResetPassword:
		purposeLabel = "重置密码"
	default:
		purposeLabel = "验证"
	}
	ttlMin := int(ttl.Round(time.Minute) / time.Minute)
	if ttlMin <= 0 {
		ttlMin = 10
	}
	ttlStr := fmt.Sprintf("%d 分钟", ttlMin)
	subject = fmt.Sprintf("WatchTogether %s验证码", purposeLabel)
	html = fmt.Sprintf(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="font-family:system-ui,-apple-system,sans-serif;line-height:1.5;color:#111827;background:#f9fafb;padding:24px;">
<div style="max-width:480px;margin:0 auto;background:#fff;border-radius:12px;padding:28px 24px;box-shadow:0 1px 3px rgba(0,0,0,.08);">
<p style="margin:0 0 12px;font-size:15px;">您好，</p>
<p style="margin:0 0 20px;font-size:15px;">您正在进行 <strong>%s</strong>。请在 <strong>%s</strong> 内使用以下验证码完成操作：</p>
<p style="margin:0 0 24px;font-size:32px;letter-spacing:8px;font-weight:700;text-align:center;color:#111827;">%s</p>
<p style="margin:0;font-size:13px;color:#6b7280;">如非本人操作，请忽略此邮件。请勿向他人透露验证码。</p>
</div>
<p style="text-align:center;font-size:12px;color:#9ca3af;margin-top:16px;">WatchTogether</p>
</body></html>`, purposeLabel, ttlStr, code)

	text = fmt.Sprintf("WatchTogether %s\n\n验证码：%s\n\n验证码在 %s 内有效。如非本人操作请忽略。\n", purposeLabel, code, ttlStr)
	return subject, html, text
}
