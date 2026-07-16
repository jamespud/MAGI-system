package server

import (
	"context"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/jamespud/magi/backend/server/dto"
)

func respondError(c *app.RequestContext, err error) {
	msg := err.Error()
	status := consts.StatusInternalServerError
	if strings.Contains(msg, "not found") || strings.Contains(msg, "not configured") {
		status = consts.StatusNotFound
	} else if strings.Contains(msg, "invalid") || strings.Contains(msg, "required") {
		status = consts.StatusBadRequest
	}
	c.JSON(status, dto.ErrorResponse{Error: msg})
}

func nopHandler(msg string) func(ctx context.Context, c *app.RequestContext) {
	return func(ctx context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusNotImplemented, dto.ErrorResponse{Error: msg + " not yet implemented"})
	}
}
