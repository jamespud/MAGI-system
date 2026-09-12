package magi

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

type userRepo struct{ db *gorm.DB }

// NewUserRepository returns a DB-backed UserRepository.
func NewUserRepository(db *gorm.DB) port.UserRepository {
	return &userRepo{db: db}
}

func (r *userRepo) Create(ctx context.Context, u *entity.User) error {
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now()
	}
	u.UpdatedAt = u.CreatedAt
	if u.Status == "" {
		u.Status = entity.UserStatusActive
	}
	m := UserModel{
		Name: u.Name, Email: u.Email, Role: u.Role, Status: u.Status,
		AuthVersion: u.AuthVersion, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return err
	}
	u.ID = m.ID
	return nil
}

func (r *userRepo) GetByID(ctx context.Context, id int64) (*entity.User, error) {
	var m UserModel
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, port.ErrUserNotFound
		}
		return nil, err
	}
	return userFromModel(&m), nil
}

func (r *userRepo) FindByEmail(ctx context.Context, email string) (*entity.User, error) {
	var m UserModel
	if err := r.db.WithContext(ctx).Where("email = ?", strings.TrimSpace(email)).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, port.ErrUserNotFound
		}
		return nil, err
	}
	return userFromModel(&m), nil
}

func (r *userRepo) List(ctx context.Context) ([]*entity.User, error) {
	var models []UserModel
	if err := r.db.WithContext(ctx).Order("id ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.User, len(models))
	for i := range models {
		out[i] = userFromModel(&models[i])
	}
	return out, nil
}

func (r *userRepo) Update(ctx context.Context, u *entity.User) error {
	u.UpdatedAt = time.Now()
	res := r.db.WithContext(ctx).Model(&UserModel{}).Where("id = ?", u.ID).Updates(map[string]any{
		"name": u.Name, "email": u.Email, "role": u.Role, "updated_at": u.UpdatedAt,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		if _, err := r.GetByID(ctx, u.ID); err != nil {
			return err
		}
	}
	return nil
}

func (r *userRepo) Delete(ctx context.Context, id int64) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&UserModel{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return port.ErrUserNotFound
	}
	return nil
}

// BumpAuthVersion invalidates every existing session for the user.
func (r *userRepo) BumpAuthVersion(ctx context.Context, id int64) (int64, error) {
	return r.mutate(ctx, id, nil)
}

// ApplyUserMutation writes every populated field plus (optionally) an atomic
// auth_version increment in a single UPDATE, so a multi-field admin patch can
// never be partially applied.
func (r *userRepo) ApplyUserMutation(ctx context.Context, id int64, m port.UserMutation) (*entity.User, error) {
	updates := map[string]any{"updated_at": time.Now()}
	if m.Name != nil {
		updates["name"] = *m.Name
	}
	if m.Email != nil {
		updates["email"] = *m.Email
	}
	if m.Role != nil {
		updates["role"] = *m.Role
	}
	if m.Status != nil {
		updates["status"] = *m.Status
	}
	if m.BumpAuthVersion {
		updates["auth_version"] = gorm.Expr("auth_version + 1")
	}
	res := r.db.WithContext(ctx).Model(&UserModel{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		// MySQL reports 0 affected rows when nothing actually changed, so fall
		// back to an existence check rather than reporting a false not-found.
		if _, err := r.GetByID(ctx, id); err != nil {
			return nil, err
		}
	}
	return r.GetByID(ctx, id)
}

// mutate applies an atomic auth_version increment together with any extra
// columns, so no reader can observe new authorization facts paired with the
// previous version.
func (r *userRepo) mutate(ctx context.Context, id int64, extra map[string]any) (int64, error) {
	updates := map[string]any{
		"auth_version": gorm.Expr("auth_version + 1"),
		"updated_at":   time.Now(),
	}
	for k, v := range extra {
		updates[k] = v
	}
	res := r.db.WithContext(ctx).Model(&UserModel{}).Where("id = ?", id).Updates(updates)
	if res.Error != nil {
		return 0, res.Error
	}
	if res.RowsAffected == 0 {
		return 0, port.ErrUserNotFound
	}
	var m UserModel
	if err := r.db.WithContext(ctx).Select("auth_version").Where("id = ?", id).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, port.ErrUserNotFound
		}
		return 0, err
	}
	return m.AuthVersion, nil
}

func userFromModel(m *UserModel) *entity.User {
	return &entity.User{
		ID: m.ID, Name: m.Name, Email: m.Email, Role: m.Role, Status: m.Status,
		AuthVersion: m.AuthVersion, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

type apiKeyRepo struct{ db *gorm.DB }

// NewApiKeyRepository returns a DB-backed ApiKeyRepository.
func NewApiKeyRepository(db *gorm.DB) port.ApiKeyRepository {
	return &apiKeyRepo{db: db}
}

func (r *apiKeyRepo) Create(ctx context.Context, k *entity.ApiKey) error {
	if k.CreatedAt.IsZero() {
		k.CreatedAt = time.Now()
	}
	return r.db.WithContext(ctx).Create(apiKeyToModel(k)).Error
}

func (r *apiKeyRepo) GetByID(ctx context.Context, id string) (*entity.ApiKey, error) {
	var m ApiKeyModel
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		return nil, err
	}
	return apiKeyFromModel(&m), nil
}

func (r *apiKeyRepo) ListByUser(ctx context.Context, userID int64) ([]*entity.ApiKey, error) {
	var models []ApiKeyModel
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Order("created_at DESC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.ApiKey, len(models))
	for i := range models {
		out[i] = apiKeyFromModel(&models[i])
	}
	return out, nil
}

func (r *apiKeyRepo) FindByKeyHash(ctx context.Context, hash string) (*entity.ApiKey, error) {
	var m ApiKeyModel
	if err := r.db.WithContext(ctx).Where("key_hash = ?", hash).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, port.ErrAPIKeyNotFound
		}
		return nil, err
	}
	return apiKeyFromModel(&m), nil
}

func (r *apiKeyRepo) Update(ctx context.Context, k *entity.ApiKey) error {
	return r.db.WithContext(ctx).Save(apiKeyToModel(k)).Error
}

func (r *apiKeyRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&ApiKeyModel{}).Error
}

func apiKeyToModel(k *entity.ApiKey) *ApiKeyModel {
	return &ApiKeyModel{
		ID: k.ID, UserID: k.UserID, Name: k.Name, Prefix: k.Prefix,
		KeyHash: k.KeyHash, LastUsedAt: k.LastUsedAt, Revoked: k.Revoked, CreatedAt: k.CreatedAt,
	}
}

func apiKeyFromModel(m *ApiKeyModel) *entity.ApiKey {
	return &entity.ApiKey{
		ID: m.ID, UserID: m.UserID, Name: m.Name, Prefix: m.Prefix,
		KeyHash: m.KeyHash, LastUsedAt: m.LastUsedAt, Revoked: m.Revoked, CreatedAt: m.CreatedAt,
	}
}
