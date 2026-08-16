package magi

import (
	"context"
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
	m := UserModel{Name: u.Name, Role: u.Role, CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return err
	}
	u.ID = m.ID
	return nil
}

func (r *userRepo) GetByID(ctx context.Context, id int64) (*entity.User, error) {
	var m UserModel
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		return nil, err
	}
	return &entity.User{ID: m.ID, Name: m.Name, Role: m.Role, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}, nil
}

func (r *userRepo) List(ctx context.Context) ([]*entity.User, error) {
	var models []UserModel
	if err := r.db.WithContext(ctx).Order("id ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	out := make([]*entity.User, len(models))
	for i := range models {
		m := &models[i]
		out[i] = &entity.User{ID: m.ID, Name: m.Name, Role: m.Role, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
	}
	return out, nil
}

func (r *userRepo) Update(ctx context.Context, u *entity.User) error {
	u.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Model(&UserModel{}).Where("id = ?", u.ID).Updates(map[string]any{
		"name": u.Name, "role": u.Role, "updated_at": u.UpdatedAt,
	}).Error
}

func (r *userRepo) Delete(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&UserModel{}).Error
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
