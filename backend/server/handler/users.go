package handler

import (
	"context"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/jamespud/magi/backend/application/auth"
	"github.com/jamespud/magi/backend/application/users"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/server/dto"
)

// UsersHandler serves user and API-key management endpoints.
type UsersHandler struct {
	svc *users.Service
}

func NewUsersHandler(svc *users.Service) *UsersHandler {
	return &UsersHandler{svc: svc}
}

// actorRole returns the caller's role. Open mode (auth disabled, nil
// principal) is treated as admin so local development keeps working.
func actorRole(ctx context.Context) string {
	if p := auth.PrincipalFrom(ctx); p != nil {
		return p.Role
	}
	return entity.RoleAdmin
}

func (h *UsersHandler) CreateUser(ctx context.Context, c *app.RequestContext) {
	var req dto.CreateUserRequest
	if err := c.BindAndValidate(&req); err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "name is required"})
		return
	}
	u, key, err := h.svc.CreateUser(ctx, actorRole(ctx), req.Name, req.Role)
	if err != nil {
		writeUsersError(c, err)
		return
	}
	var k *dto.IssuedKeyDTO
	if key != nil {
		k = &dto.IssuedKeyDTO{ID: key.ID, Prefix: key.Prefix, Plaintext: key.Plaintext}
	}
	c.JSON(consts.StatusCreated, dto.CreateUserResponse{User: dto.FromUser(u, 0), ApiKey: k})
}

func (h *UsersHandler) ListUsers(ctx context.Context, c *app.RequestContext) {
	summaries, err := h.svc.ListUsers(ctx, actorRole(ctx))
	if err != nil {
		writeUsersError(c, err)
		return
	}
	out := make([]dto.UserDTO, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, dto.FromUser(s.User, s.ActiveKeys))
	}
	c.JSON(consts.StatusOK, dto.UserListResponse{Users: out})
}

func (h *UsersHandler) DeleteUser(ctx context.Context, c *app.RequestContext) {
	id, err := userIDParam(c)
	if err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "invalid user id"})
		return
	}
	if err := h.svc.DeleteUser(ctx, actorRole(ctx), id); err != nil {
		writeUsersError(c, err)
		return
	}
	c.Status(consts.StatusNoContent)
}

func (h *UsersHandler) ListKeys(ctx context.Context, c *app.RequestContext) {
	id, err := userIDParam(c)
	if err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "invalid user id"})
		return
	}
	actorID := CurrentUserID(ctx)
	keys, err := h.svc.ListKeys(ctx, actorID, actorRole(ctx), id)
	if err != nil {
		writeUsersError(c, err)
		return
	}
	out := make([]dto.ApiKeyDTO, 0, len(keys))
	for _, k := range keys {
		out = append(out, dto.FromApiKey(k))
	}
	c.JSON(consts.StatusOK, dto.KeyListResponse{Keys: out})
}

func (h *UsersHandler) IssueKey(ctx context.Context, c *app.RequestContext) {
	id, err := userIDParam(c)
	if err != nil {
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: "invalid user id"})
		return
	}
	var req dto.IssueKeyRequest
	_ = c.BindAndValidate(&req)
	key, err := h.svc.IssueKey(ctx, CurrentUserID(ctx), actorRole(ctx), id, req.Name)
	if err != nil {
		writeUsersError(c, err)
		return
	}
	c.JSON(consts.StatusCreated, dto.IssuedKeyDTO{ID: key.ID, Prefix: key.Prefix, Plaintext: key.Plaintext})
}

// IssueOwnKey issues a key for the calling user (self-service).
func (h *UsersHandler) IssueOwnKey(ctx context.Context, c *app.RequestContext) {
	var req dto.IssueKeyRequest
	_ = c.BindAndValidate(&req)
	userID := CurrentUserID(ctx)
	if userID == 0 {
		c.JSON(consts.StatusUnauthorized, dto.ErrorResponse{Error: "unauthorized"})
		return
	}
	key, err := h.svc.IssueKey(ctx, userID, actorRole(ctx), userID, req.Name)
	if err != nil {
		writeUsersError(c, err)
		return
	}
	c.JSON(consts.StatusCreated, dto.IssuedKeyDTO{ID: key.ID, Prefix: key.Prefix, Plaintext: key.Plaintext})
}

func (h *UsersHandler) RevokeKey(ctx context.Context, c *app.RequestContext) {
	if err := h.svc.RevokeKey(ctx, CurrentUserID(ctx), actorRole(ctx), c.Param("id")); err != nil {
		writeUsersError(c, err)
		return
	}
	c.Status(consts.StatusNoContent)
}

func (h *UsersHandler) RotateKey(ctx context.Context, c *app.RequestContext) {
	key, err := h.svc.RotateKey(ctx, CurrentUserID(ctx), actorRole(ctx), c.Param("id"))
	if err != nil {
		writeUsersError(c, err)
		return
	}
	c.JSON(consts.StatusOK, dto.IssuedKeyDTO{ID: key.ID, Prefix: key.Prefix, Plaintext: key.Plaintext})
}

func (h *UsersHandler) Me(ctx context.Context, c *app.RequestContext) {
	userID := CurrentUserID(ctx)
	if userID == 0 {
		c.JSON(consts.StatusUnauthorized, dto.ErrorResponse{Error: "unauthorized"})
		return
	}
	u, keys, err := h.svc.Me(ctx, userID)
	if err != nil {
		writeUsersError(c, err)
		return
	}
	out := dto.MeResponse{User: dto.FromUser(u, 0), Keys: make([]dto.ApiKeyDTO, 0, len(keys))}
	for _, k := range keys {
		out.Keys = append(out.Keys, dto.FromApiKey(k))
	}
	c.JSON(consts.StatusOK, out)
}

func userIDParam(c *app.RequestContext) (int64, error) {
	return strconv.ParseInt(c.Param("id"), 10, 64)
}

func writeUsersError(c *app.RequestContext, err error) {
	switch err {
	case users.ErrForbidden:
		Forbidden(c)
	case users.ErrNotFound:
		c.JSON(consts.StatusNotFound, dto.ErrorResponse{Error: "not found"})
	default:
		c.JSON(consts.StatusBadRequest, dto.ErrorResponse{Error: err.Error()})
	}
}
