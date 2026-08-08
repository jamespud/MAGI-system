package handler

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/server/dto"
)

// AuthorizeCase returns true when the authenticated principal may access a
// resource owned by ownerID. Open mode (nil principal or owner 0) allows all.
func AuthorizeCase(ctx context.Context, ownerID int64) bool {
	return auth.CanAccess(ctx, ownerID)
}

// CurrentUserID returns the authenticated user id, or 0 in open mode.
func CurrentUserID(ctx context.Context) int64 {
	if p := auth.PrincipalFrom(ctx); p != nil {
		return p.UserID
	}
	return 0
}

// Forbidden writes a 403 response.
func Forbidden(c *app.RequestContext) {
	c.JSON(consts.StatusForbidden, dto.ErrorResponse{Error: "forbidden"})
}

// CaseAllowed returns true when the principal may access a case. Unknown
// cases are allowed only in open mode (nil principal), preserving local/dev
// behavior while enforcing ownership in production.
func CaseAllowed(ctx context.Context, cs *entity.DecisionCase) bool {
	if cs == nil {
		return auth.PrincipalFrom(ctx) == nil
	}
	return auth.CanAccess(ctx, cs.UserID)
}

// CaseGetter resolves a case for ownership checks. decision.Service satisfies it.
type CaseGetter interface {
	Get(ctx context.Context, id string) (*entity.DecisionCase, error)
}
