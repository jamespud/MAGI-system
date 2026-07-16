package server

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/jamespud/magi/backend/server/dto"
)

func RequestID() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		rid := fmt.Sprintf("req-%d", time.Now().UnixNano())
		c.Header("X-Request-ID", rid)
		c.Set("request_id", rid)
		c.Next(ctx)
	}
}

func Recovery() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		defer func() {
			if r := recover(); r != nil {
				c.JSON(consts.StatusInternalServerError, dto.ErrorResponse{
					Error: fmt.Sprintf("internal error: %v", r),
				})
				c.Abort()
			}
		}()
		c.Next(ctx)
	}
}
