package bootstrap

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// InstallDecisionWriterContract is the S29 upgrade for already migrated T1-T4
// databases. Partial prior migrations are rejected, never silently repaired.
func InstallDecisionWriterContract(ctx context.Context, db *gorm.DB) error {
	if err := VerifyDecisionGenerationSchema(ctx, db); err != nil {
		return err
	}
	if db.Migrator().HasTable(&magi.DecisionWriterContractModel{}) {
		return VerifyDecisionWriterSchema(ctx, db)
	}
	if err := db.WithContext(ctx).AutoMigrate(&magi.DecisionWriterContractModel{}); err != nil {
		return err
	}
	if err := db.WithContext(ctx).Create(&magi.DecisionWriterContractModel{ID: 1, LegacyRollbackForbidden: true}).Error; err != nil {
		return err
	}
	return VerifyDecisionWriterSchema(ctx, db)
}

var accountPart = regexp.MustCompile("^[a-zA-Z0-9_.%:-]+$")

func cutoverAccount(account string) (string, string, error) {
	parts := strings.Split(account, "@")
	if len(parts) != 2 || !accountPart.MatchString(parts[0]) || !accountPart.MatchString(parts[1]) || strings.EqualFold(parts[0], "root") {
		return "", "", fmt.Errorf("%w: require a named non-root user@host", port.ErrCutoverPrecondition)
	}
	return parts[0], parts[1], nil
}
func sqlAccount(account string) (string, error) {
	u, h, err := cutoverAccount(account)
	if err != nil {
		return "", err
	}
	return "'" + u + "'@'" + h + "'", nil
}
func ConfigureDecisionWriter(ctx context.Context, db *gorm.DB, writer, legacy string) error {
	wu, _, err := cutoverAccount(writer)
	if err != nil {
		return err
	}
	lu, _, err := cutoverAccount(legacy)
	if err != nil {
		return err
	}
	if wu == lu {
		return fmt.Errorf("%w: writer identities must have different usernames", port.ErrCutoverPrecondition)
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		gate, err := lockCutover(tx)
		if err != nil {
			return err
		}
		if gate.AdmissionState != magi.DecisionAdmissionBlocked {
			return port.ErrCutoverPrecondition
		}
		if err = nextCutover(gate); err != nil {
			return err
		}
		gate.WriterAccount, gate.LegacyAccount = writer, legacy
		return tx.Save(gate).Error
	})
}

// ProvisionDecisionWriter grants only per-table DML and control-table SELECT.
// Passwords arrive through a private environment variable, never command flags.
func ProvisionDecisionWriter(ctx context.Context, db *gorm.DB, account, password string) error {
	if password == "" {
		return port.ErrCutoverPrecondition
	}
	a, err := sqlAccount(account)
	if err != nil {
		return err
	}
	// Require a new identity; an existing account may retain global/role grants.
	var sqlMode string
	if err = db.WithContext(ctx).Raw("SELECT @@SESSION.sql_mode").Scan(&sqlMode).Error; err != nil {
		return err
	}
	escaped := password
	if !strings.Contains(sqlMode, "NO_BACKSLASH_ESCAPES") {
		escaped = strings.ReplaceAll(escaped, "\\", "\\\\")
	}
	escaped = strings.ReplaceAll(escaped, "'", "''")
	if err = db.WithContext(ctx).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)}).Exec("CREATE USER " + a + " IDENTIFIED BY '" + escaped + "'").Error; err != nil {
		return err
	}
	var schema string
	if err = db.Raw("SELECT DATABASE()").Scan(&schema).Error; err != nil {
		return err
	}
	var tables []string
	if err = db.Raw("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_TYPE='BASE TABLE'").Scan(&tables).Error; err != nil {
		return err
	}
	quote := func(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }
	for _, table := range tables {
		priv := "SELECT, INSERT, UPDATE, DELETE"
		if table == "decision_writer_contract" {
			priv = "SELECT"
		}
		if err = db.WithContext(ctx).Exec("GRANT " + priv + " ON " + quote(schema) + "." + quote(table) + " TO " + a).Error; err != nil {
			return err
		}
	}
	return nil
}

// ExcludeLegacyWriter is deliberately nontransactional because MySQL account
// DDL commits implicitly. Block first; any partial failure remains fail closed.
func ExcludeLegacyWriter(ctx context.Context, db *gorm.DB) error {
	var gate magi.DecisionWriterContractModel
	if err := db.WithContext(ctx).Where("id=1").First(&gate).Error; err != nil {
		return err
	}
	if gate.AdmissionState != magi.DecisionAdmissionBlocked {
		return port.ErrCutoverPrecondition
	}
	a, err := sqlAccount(gate.LegacyAccount)
	if err != nil {
		return err
	}
	user, _, _ := cutoverAccount(gate.LegacyAccount)
	if err = db.WithContext(ctx).Exec("ALTER USER " + a + " ACCOUNT LOCK").Error; err != nil {
		return err
	}
	if err = db.WithContext(ctx).Exec("REVOKE ALL PRIVILEGES, GRANT OPTION FROM " + a).Error; err != nil {
		return err
	}
	var ids []uint64
	if err = db.WithContext(ctx).Raw("SELECT ID FROM information_schema.PROCESSLIST WHERE USER=?", user).Scan(&ids).Error; err != nil {
		return err
	}
	for _, id := range ids {
		// A session may exit concurrently; only ignore MySQL's unknown thread.
		if err = db.WithContext(ctx).Exec(fmt.Sprintf("KILL CONNECTION %d", id)).Error; err != nil && !strings.Contains(err.Error(), "Unknown thread id") {
			return err
		}
	}
	return verifyLegacyExcluded(ctx, db, gate.LegacyAccount)
}
func verifyLegacyExcluded(ctx context.Context, db *gorm.DB, account string) error {
	u, h, err := cutoverAccount(account)
	if err != nil {
		return err
	}
	var locked string
	if err = db.WithContext(ctx).Raw("SELECT account_locked FROM mysql.user WHERE User=? AND Host=?", u, h).Scan(&locked).Error; err != nil {
		return err
	}
	if locked != "Y" {
		return fmt.Errorf("%w: legacy account is not locked", port.ErrCutoverPrecondition)
	}
	a, _ := sqlAccount(account)
	var grants []string
	if err = db.WithContext(ctx).Raw("SHOW GRANTS FOR " + a).Scan(&grants).Error; err != nil {
		return err
	}
	if len(grants) != 1 || !strings.HasPrefix(grants[0], "GRANT USAGE ON *.* TO ") {
		return fmt.Errorf("%w: legacy account retains grants or roles", port.ErrCutoverPrecondition)
	}
	var sessions int64
	if err = db.WithContext(ctx).Raw("SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE USER=?", u).Scan(&sessions).Error; err != nil {
		return err
	}
	if sessions != 0 {
		return fmt.Errorf("%w: legacy sessions remain", port.ErrCutoverPrecondition)
	}
	return nil
}

func verifyWriterPrivileges(ctx context.Context, db *gorm.DB, account string) error {
	a, err := sqlAccount(account)
	if err != nil {
		return err
	}
	var schema string
	if err = db.WithContext(ctx).Raw("SELECT DATABASE()").Scan(&schema).Error; err != nil {
		return err
	}
	var tables []string
	if err = db.WithContext(ctx).Raw("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_TYPE='BASE TABLE'").Scan(&tables).Error; err != nil {
		return err
	}
	quote := func(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }
	required := map[string]string{}
	for _, table := range tables {
		privileges := "SELECT, INSERT, UPDATE, DELETE"
		if table == "decision_writer_contract" {
			privileges = "SELECT"
		}
		required[quote(schema)+"."+quote(table)] = privileges
	}
	var grants []string
	if err = db.WithContext(ctx).Raw("SHOW GRANTS FOR " + a).Scan(&grants).Error; err != nil {
		return err
	}
	for _, grant := range grants {
		if strings.HasPrefix(grant, "GRANT USAGE ON *.* TO ") {
			continue
		}
		parts := strings.SplitN(grant, " ON ", 2)
		if len(parts) != 2 || !strings.HasPrefix(parts[0], "GRANT ") {
			return port.ErrDecisionWriterIdentity
		}
		target := strings.SplitN(parts[1], " TO ", 2)
		if len(target) != 2 || required[target[0]] != strings.TrimPrefix(parts[0], "GRANT ") || strings.Contains(target[1], "WITH GRANT OPTION") {
			return port.ErrDecisionWriterIdentity
		}
		delete(required, target[0])
	}
	// Validating only the grants present also accepts USAGE-only accounts or
	// omitted tables. Every required table must have its exact privilege set.
	if len(required) != 0 {
		return port.ErrDecisionWriterIdentity
	}
	return nil
}

func VerifyDecisionCutover(ctx context.Context, db *gorm.DB) error {
	if err := VerifyDecisionWriterSchema(ctx, db); err != nil {
		return err
	}
	var gate magi.DecisionWriterContractModel
	if err := db.WithContext(ctx).Where("id=1").First(&gate).Error; err != nil {
		return err
	}
	if _, _, err := cutoverAccount(gate.WriterAccount); err != nil {
		return err
	}
	if err := verifyWriterPrivileges(ctx, db, gate.WriterAccount); err != nil {
		return err
	}
	if err := verifyLegacyExcluded(ctx, db, gate.LegacyAccount); err != nil {
		return err
	}
	var running int64
	if err := db.WithContext(ctx).Model(&magi.DecisionJobModel{}).Where("status='RUNNING'").Count(&running).Error; err != nil {
		return err
	}
	if running != 0 {
		return fmt.Errorf("%w: %d RUNNING jobs must drain or be explicitly cancelled", port.ErrCutoverPrecondition, running)
	}
	return nil
}

// EnableDecisionWriter uses the exact blocked epoch so a stale operator cannot
// reopen a later cutover. No operation resets the irreversible rollback marker.
func EnableDecisionWriter(ctx context.Context, db *gorm.DB, expectedEpoch uint64) error {
	if err := VerifyDecisionCutover(ctx, db); err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		gate, err := lockCutover(tx)
		if err != nil {
			return err
		}
		if gate.AdmissionState != magi.DecisionAdmissionBlocked || gate.CutoverEpoch != expectedEpoch || gate.ContractVersion != port.DecisionWriterContractVersion {
			return port.ErrCutoverPrecondition
		}
		// While the exclusive gate lock is held, no new Claim can begin.
		var running int64
		if err = tx.Model(&magi.DecisionJobModel{}).Where("status='RUNNING'").Count(&running).Error; err != nil {
			return err
		}
		if running != 0 {
			return port.ErrCutoverPrecondition
		}
		if err = nextCutover(gate); err != nil {
			return err
		}
		gate.AdmissionState = magi.DecisionAdmissionEnabled
		gate.LegacyRollbackForbidden = true
		return tx.Save(gate).Error
	})
}
