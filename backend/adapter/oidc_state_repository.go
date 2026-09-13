package magi

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/jamespud/magi/backend/domain/port"
)

type oidcStateRepo struct{ db *gorm.DB }

// NewOIDCStateRepository returns the database-backed OIDC state store.
func NewOIDCStateRepository(db *gorm.DB) port.OIDCStateRepository {
	return &oidcStateRepo{db: db}
}

func (r *oidcStateRepo) Issue(ctx context.Context, state string, expiresAt time.Time) error {
	// Opportunistic cleanup keeps the table small without a background sweeper.
	_ = r.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&OIDCStateModel{}).Error
	return r.db.WithContext(ctx).Create(&OIDCStateModel{State: state, ExpiresAt: expiresAt}).Error
}

// Consume claims a state exactly once. The conditional UPDATE is the fence: only
// the statement that flips consumed_at away from NULL reports RowsAffected == 1,
// so two replicas racing on the same callback cannot both succeed.
func (r *oidcStateRepo) Consume(ctx context.Context, state string) (bool, error) {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&OIDCStateModel{}).
		Where("state = ? AND consumed_at IS NULL AND expires_at > ?", state, now).
		Updates(map[string]any{"consumed_at": now})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

var _ port.OIDCStateRepository = (*oidcStateRepo)(nil)
