package bootstrap

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
)

func TestMySQLDecisionWriter_GateDeniesAdmissionAndClaim(t *testing.T) {
	for _, state := range []string{"BLOCKED", "missing", "version", "identity"} {
		t.Run(state, func(t *testing.T) {
			_, db := t6SchemaFixture(t)
			ctx := context.Background()
			repo := magi.NewRepository(db)
			jobs := magi.NewDecisionJobRepository(db)
			c := &entity.DecisionCase{ID: uuid.NewString(), Status: entity.CaseStatusDraft}
			if err := repo.CaseRepo().Create(ctx, c); err != nil {
				t.Fatal(err)
			}
			job, ok, err := jobs.Admit(ctx, c.ID, 3, 0)
			if err != nil || !ok {
				t.Fatalf("positive control: %v", err)
			}
			token := uuid.NewString()
			first, ok, err := jobs.Claim(ctx, job.ID, "w", token, time.Now().Add(time.Minute))
			if err != nil || !ok || first.ExecutionGeneration != 1 {
				t.Fatalf("claim positive control: %v", err)
			}
			expected := port.ErrDecisionWriterBlocked
			switch state {
			case "missing":
				err = db.Exec("DELETE FROM decision_writer_contract").Error
			case "version":
				err = db.Exec("UPDATE decision_writer_contract SET contract_version=99").Error
				expected = port.ErrDecisionWriterVersion
			case "identity":
				err = db.Exec("UPDATE decision_writer_contract SET writer_account='other@%'").Error
				expected = port.ErrDecisionWriterIdentity
			default:
				err = db.Exec("UPDATE decision_writer_contract SET admission_state='BLOCKED'").Error
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = jobs.Admit(ctx, c.ID, 3, 0); !errors.Is(err, expected) {
				t.Errorf("admit accepted closed gate: %v", err)
			}
			// Even a previously committed stable token is not permission to launch while blocked.
			if _, _, err = jobs.Claim(ctx, job.ID, "w", token, time.Now().Add(time.Minute)); !errors.Is(err, expected) {
				t.Errorf("claim accepted closed gate: %v", err)
			}
			after, _ := repo.CaseRepo().Get(ctx, c.ID)
			current, _ := jobs.GetByCase(ctx, c.ID)
			if after.ExecutionGeneration != 1 || current.ExecutionGeneration != 1 || current.Status != entity.DecisionJobRunning {
				t.Fatal("gate rejection changed authority")
			}
		})
	}
}
