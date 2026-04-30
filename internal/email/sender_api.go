package email

import "context"

// SenderAPI is implemented by Sender for dependency injection and tests.
type SenderAPI interface {
	Enabled() bool
	SendVerificationCode(ctx context.Context, to, purpose, code string) (emailID string, err error)
}
