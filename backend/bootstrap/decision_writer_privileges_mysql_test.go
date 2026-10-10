package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestMySQLDecisionWriter_RequiredPrivileges(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		usageOnly bool
		change    string
	}{
		{name: "usage_only", usageOnly: true},
		{name: "missing_business_table", change: "REVOKE SELECT, INSERT, UPDATE, DELETE ON %s.`decision_case` FROM %s"},
		{name: "partial_dml", change: "REVOKE DELETE ON %s.`decision_case` FROM %s"},
		{name: "missing_contract_select", change: "REVOKE SELECT ON %s.`decision_writer_contract` FROM %s"},
		{name: "extra_contract_update", change: "GRANT UPDATE ON %s.`decision_writer_contract` TO %s"},
		{name: "minimal_permissions"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, db := t6SchemaFixture(t)
			ctx := context.Background()
			seedT6Result(t, db)
			legacy, _ := t6Account(t, db, "t6_old_")
			var writer string
			if scenario.usageOnly {
				writer, _ = t6Account(t, db, "t6_new_")
			} else {
				writer = "t6_new_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:14] + "@%"
				if err := ProvisionDecisionWriter(ctx, db, writer, uuid.NewString()); err != nil {
					t.Fatal(err)
				}
				account, err := sqlAccount(writer)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Exec("DROP USER IF EXISTS " + account).Error })
			}
			if _, err := BlockDecisionWriter(ctx, db); err != nil {
				t.Fatal(err)
			}
			if err := ConfigureDecisionWriter(ctx, db, writer, legacy); err != nil {
				t.Fatal(err)
			}
			if err := ExcludeLegacyWriter(ctx, db); err != nil {
				t.Fatal(err)
			}
			if scenario.change != "" {
				var schema string
				if err := db.Raw("SELECT DATABASE()").Scan(&schema).Error; err != nil {
					t.Fatal(err)
				}
				account, err := sqlAccount(writer)
				if err != nil {
					t.Fatal(err)
				}
				statement := fmt.Sprintf(scenario.change, "`"+strings.ReplaceAll(schema, "`", "``")+"`", account)
				if err := db.Exec(statement).Error; err != nil {
					t.Fatal(err)
				}
			}
			var before magi.DecisionWriterContractModel
			if err := db.First(&before, 1).Error; err != nil {
				t.Fatal(err)
			}
			if before.AdmissionState != magi.DecisionAdmissionBlocked {
				t.Fatal("fixture must start blocked")
			}
			authority := t6Snapshot(t, db)
			valid := scenario.name == "minimal_permissions"
			verifyErr := VerifyDecisionCutover(ctx, db)
			enableErr := EnableDecisionWriter(ctx, db, before.CutoverEpoch)
			if valid {
				if verifyErr != nil || enableErr != nil {
					t.Fatalf("minimal writer rejected: verify=%v enable=%v", verifyErr, enableErr)
				}
			} else {
				if !errors.Is(verifyErr, port.ErrDecisionWriterIdentity) {
					t.Errorf("verify: want identity error, got %v", verifyErr)
				}
				if !errors.Is(enableErr, port.ErrDecisionWriterIdentity) {
					t.Errorf("enable: want identity error, got %v", enableErr)
				}
			}
			var after magi.DecisionWriterContractModel
			if err := db.First(&after, 1).Error; err != nil {
				t.Fatal(err)
			}
			if valid {
				if after.AdmissionState != magi.DecisionAdmissionEnabled || after.CutoverEpoch != before.CutoverEpoch+1 || !after.LegacyRollbackForbidden {
					t.Errorf("minimal writer did not enable exactly one epoch: %+v", after)
				}
				if err := EnableDecisionWriter(ctx, db, before.CutoverEpoch); !errors.Is(err, port.ErrCutoverPrecondition) {
					t.Errorf("duplicate enable: want precondition error, got %v", err)
				}
				var repeated magi.DecisionWriterContractModel
				if err := db.First(&repeated, 1).Error; err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(after, repeated) {
					t.Error("duplicate enable changed gate")
				}
			} else if !reflect.DeepEqual(before, after) {
				t.Errorf("rejected writer changed gate: before=%+v after=%+v", before, after)
			}
			if !reflect.DeepEqual(authority, t6Snapshot(t, db)) {
				t.Error("privilege verification/enablement changed authoritative records")
			}
		})
	}
}
