package email

import "errors"

var ErrDisabled = errors.New("email: resend is not configured")
