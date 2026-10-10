// decision-admin is offline control-plane tooling. It never starts workers.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/bootstrap"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: decision-admin {init|install-contract|verify-schema|verify-cutover|verify-writer|status|block|configure|provision|exclude-legacy|enable}")
	}
	flags := flag.NewFlagSet(os.Args[1], flag.ContinueOnError)
	writer := flags.String("writer", "", "new user@host")
	legacy := flags.String("legacy", "", "old user@host")
	caseID := flags.String("case", "", "explicit Case to cancel during failed drain")
	epoch := flags.Uint64("epoch", 0, "exact blocked epoch")
	timeout := flags.Duration("timeout", time.Minute, "operation deadline")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	dsn := os.Getenv("MAGI_ADMIN_DSN")
	if os.Args[1] == "verify-writer" || os.Args[1] == "verify-identity" {
		dsn = os.Getenv("MAGI_WRITER_DSN")
	}
	if dsn == "" {
		return fmt.Errorf("private MAGI_ADMIN_DSN (or MAGI_WRITER_DSN for verify-writer) is required")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return fmt.Errorf("database connection failed")
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer pool.Close()
	switch os.Args[1] {
	case "init":
		return bootstrap.PrepareDecisionWriterDatabase(ctx, db)
	case "install-contract":
		return bootstrap.InstallDecisionWriterContract(ctx, db)
	case "verify-schema":
		return bootstrap.VerifyDecisionWriterSchema(ctx, db)
	case "verify-cutover":
		return bootstrap.VerifyDecisionCutover(ctx, db)
	case "cancel-case":
		if *caseID == "" {
			return fmt.Errorf("--case is required")
		}
		if err = bootstrap.VerifyDecisionGenerationSchema(ctx, db); err != nil {
			return err
		}
		_, err = magi.NewDecisionWriterRepository(db).(port.CaseControlCommitter).CommitCaseControl(ctx, *caseID, entity.CaseStatusCancelled)
		return err
	case "running-count":
		var n int64
		if err = db.WithContext(ctx).Model(&magi.DecisionJobModel{}).Where("status='RUNNING'").Count(&n).Error; err != nil {
			return err
		}
		fmt.Println(n)
		return nil
	case "verify-identity":
		if err = bootstrap.VerifyDecisionWriterSchema(ctx, db); err != nil {
			return err
		}
		var gate magi.DecisionWriterContractModel
		if err = db.WithContext(ctx).Where("id=1").First(&gate).Error; err != nil {
			return err
		}
		var account string
		if err = db.WithContext(ctx).Raw("SELECT CURRENT_USER()").Scan(&account).Error; err != nil {
			return err
		}
		if account != gate.WriterAccount {
			return fmt.Errorf("writer identity mismatch")
		}
		return nil
	case "verify-writer":
		if err = bootstrap.VerifyDecisionWriterSchema(ctx, db); err != nil {
			return err
		}
		return magi.NewDecisionWriterGuard(db).CheckDecisionWriter(ctx)
	case "block":
		n, e := bootstrap.BlockDecisionWriter(ctx, db)
		if e == nil {
			fmt.Println(n)
		}
		return e
	case "configure":
		return bootstrap.ConfigureDecisionWriter(ctx, db, *writer, *legacy)
	case "provision":
		return bootstrap.ProvisionDecisionWriter(ctx, db, *writer, os.Getenv("MAGI_NEW_WRITER_PASSWORD"))
	case "exclude-legacy":
		return bootstrap.ExcludeLegacyWriter(ctx, db)
	case "enable":
		return bootstrap.EnableDecisionWriter(ctx, db, *epoch)
	case "status":
		var gate magi.DecisionWriterContractModel
		if err = db.WithContext(ctx).Where("id=1").First(&gate).Error; err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(gate)
	default:
		return fmt.Errorf("unknown management command")
	}
}
