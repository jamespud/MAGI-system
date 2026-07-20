package server

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/jamespud/magi/backend/server/dto"
)

// Domain errors that map to specific HTTP status codes (§13).
var (
	ErrNotFound      = errors.New("not found")
	ErrNotConfigured = errors.New("not configured")
	ErrInvalidInput  = errors.New("invalid input")
)

// AppError wraps a domain error with an HTTP status code.
type AppError struct {
	Code    int
	Message string
}

func (e *AppError) Error() string { return e.Message }

// NewAppError creates an AppError with the given status and message.
func NewAppError(code int, msg string) *AppError {
	return &AppError{Code: code, Message: msg}
}

// respondError maps a domain/application error to an HTTP status code.
// Uses typed error checking first, falls back to string matching for
// errors that don't implement AppError.
func respondError(c *app.RequestContext, err error) {
	// Check for AppError first
	var appErr *AppError
	if errors.As(err, &appErr) {
		c.JSON(appErr.Code, dto.ErrorResponse{Error: appErr.Message})
		return
	}

	// Typed error checks
	msg := err.Error()
	status := consts.StatusInternalServerError
	switch {
	case errors.Is(err, ErrNotFound) || errors.Is(err, ErrNotConfigured):
		status = consts.StatusNotFound
	case errors.Is(err, ErrInvalidInput):
		status = consts.StatusBadRequest
	case strings.Contains(msg, "not found") || strings.Contains(msg, "not configured"):
		status = consts.StatusNotFound
	case strings.Contains(msg, "invalid") || strings.Contains(msg, "required"):
		status = consts.StatusBadRequest
	case strings.Contains(msg, "nil case") || strings.Contains(msg, "nil config"):
		status = consts.StatusBadRequest
	}
	c.JSON(status, dto.ErrorResponse{Error: msg})
}

func nopHandler(msg string) func(ctx context.Context, c *app.RequestContext) {
	return func(ctx context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusNotImplemented, dto.ErrorResponse{Error: msg + " not yet implemented"})
	}
}
