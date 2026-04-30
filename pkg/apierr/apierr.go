package apierr

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

type Code string

const (
	CodeInvalidRequest Code = "INVALID_REQUEST"
	CodeUnauthorized   Code = "UNAUTHORIZED"
	CodeForbidden      Code = "FORBIDDEN"
	CodeNotFound       Code = "NOT_FOUND"
	CodeConflict       Code = "CONFLICT"
	CodeRateLimited    Code = "RATE_LIMITED"
	CodeInternal       Code = "INTERNAL_ERROR"
)

type Error struct {
	Code    Code
	Message string
	Status  int
}

func (e *Error) Error() string {
	return e.Message
}

func New(status int, code Code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func InvalidRequest(message string) *Error {
	return New(http.StatusBadRequest, CodeInvalidRequest, message)
}

func Unauthorized(message string) *Error {
	return New(http.StatusUnauthorized, CodeUnauthorized, message)
}

func Forbidden(message string) *Error {
	return New(http.StatusForbidden, CodeForbidden, message)
}

func NotFound(message string) *Error {
	return New(http.StatusNotFound, CodeNotFound, message)
}

func Conflict(message string) *Error {
	return New(http.StatusConflict, CodeConflict, message)
}

func TooManyRequests(message string) *Error {
	return New(http.StatusTooManyRequests, CodeRateLimited, message)
}

func Internal(message string) *Error {
	return New(http.StatusInternalServerError, CodeInternal, message)
}

func Respond(c *gin.Context, err error) {
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		apiErr = Internal(http.StatusText(http.StatusInternalServerError))
	}

	c.JSON(apiErr.Status, gin.H{
		"error": gin.H{
			"code":    apiErr.Code,
			"message": apiErr.Message,
		},
	})
}

func Abort(c *gin.Context, err error) {
	Respond(c, err)
	c.Abort()
}
