package bootstrap

import (
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gorm.io/gorm/schema"

	magi "github.com/jamespud/magi/backend/adapter"
)

const s21MigrationPath = "../../docker/atlas/migrations/magi_s21_runtime_kernel.sql"
const s24MigrationPath = "../../docker/atlas/migrations/magi_s24_approval_intent_binding.sql"
const s25MigrationPath = "../../docker/atlas/migrations/magi_s25_execution_generation.sql"

// T1 (#19): the Case execution generation is persisted on both the Case and the
// job that claims it. The Atlas migration and the GORM models must agree on the
// shape, or a hand-applied schema diverges from the startup AutoMigrate path.
func TestS25ExecutionGenerationMigrationMatchesModel(t *testing.T) {
	raw, err := os.ReadFile(s25MigrationPath)
	if err != nil {
		t.Fatalf("read s25 migration: %v", err)
	}
	sql := string(raw)
	pattern := regexp.MustCompile(`(?i)ALTER TABLE\s+(decision_case|decision_job)\s+ADD COLUMN\s+execution_generation\s+BIGINT\s+NOT NULL\s+DEFAULT\s+0`)
	matched := map[string]bool{}
	for _, m := range pattern.FindAllStringSubmatch(sql, -1) {
		matched[strings.ToLower(m[1])] = true
	}
	for _, table := range []string{"decision_case", "decision_job"} {
		if !matched[table] {
			t.Fatalf("S25 must add execution_generation BIGINT NOT NULL DEFAULT 0 to %s:\n%s", table, sql)
		}
	}

	for _, tc := range []struct {
		model any
		name  string
	}{
		{&magi.CaseModel{}, "CaseModel"},
		{&magi.DecisionJobModel{}, "DecisionJobModel"},
	} {
		field, ok := reflect.TypeOf(tc.model).Elem().FieldByName("ExecutionGeneration")
		if !ok {
			t.Fatalf("%s is missing ExecutionGeneration", tc.name)
		}
		tag := field.Tag.Get("gorm")
		if !strings.Contains(tag, "not null") || !strings.Contains(tag, "default:0") {
			t.Fatalf("%s.ExecutionGeneration tag = %q, want not null and default:0", tc.name, tag)
		}
	}
}

// Issue #9: the approval identity became the logical invocation, pinned by an
// intent digest. The Atlas script and the GORM model must agree on the column
// widths and on the unique key, so a deployment that applies the SQL by hand
// does not diverge from one that relies on the startup AutoMigrate path.
func TestS24ApprovalIntentMigrationMatchesModel(t *testing.T) {
	raw, err := os.ReadFile(s24MigrationPath)
	if err != nil {
		t.Fatalf("read s24 migration: %v", err)
	}
	sql := string(raw)
	if !strings.Contains(strings.ToUpper(sql), "INTENT_DIGEST") {
		t.Fatalf("S24 must add the intent_digest column:\n%s", sql)
	}

	for _, column := range []string{"invocation_id", "intent_digest"} {
		match := regexp.MustCompile(`(?i)` + column + `\s+VARCHAR\((\d+)\)`).FindStringSubmatch(sql)
		if match == nil {
			t.Fatalf("S24 must declare %s as VARCHAR(n):\n%s", column, sql)
		}
		declared, _ := strconv.Atoi(match[1])
		if model := modelColumnSize(t, column); declared != model {
			t.Fatalf("S24 declares %s VARCHAR(%d) but ApprovalModel declares size:%d", column, declared, model)
		}
	}

	// The unique constraint is what makes "one logical call, one authoritative
	// approval request" hold under a concurrent create, so the script must
	// carry it, not only the model tag.
	if match := regexp.MustCompile(`(?i)UNIQUE KEY\s+uk_approval_invocation\s*\(\s*case_id\s*,\s*invocation_id\s*\)`).FindString(sql); match == "" {
		t.Fatalf("S24 must declare UNIQUE KEY uk_approval_invocation (case_id, invocation_id):\n%s", sql)
	}
}

// modelColumnSize returns the explicit gorm size of the model field whose
// snake_case column name matches, using GORM's own naming strategy so an ID
// field is not mangled into run__id.
func modelColumnSize(t *testing.T, column string) int {
	t.Helper()
	naming := schema.NamingStrategy{}
	rt := reflect.TypeOf(magi.ApprovalModel{})
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if naming.ColumnName("", field.Name) != column {
			continue
		}
		for _, part := range strings.Split(field.Tag.Get("gorm"), ";") {
			if strings.HasPrefix(part, "size:") {
				size, _ := strconv.Atoi(strings.TrimPrefix(part, "size:"))
				return size
			}
		}
		t.Fatalf("ApprovalModel.%s must declare an explicit size", field.Name)
	}
	t.Fatalf("ApprovalModel has no field for column %s", column)
	return 0
}

// The Atlas snapshot and the GORM models describe the same tables. Nothing kept
// them in sync, which is how run_id ended up 64 wide in both while callers wrote
// 69 characters (see docs/reliability-hazard-audit.md §2.2 and §5.2).
func TestS21MigrationMatchesModelWidths(t *testing.T) {
	raw, err := os.ReadFile(s21MigrationPath)
	if err != nil {
		t.Fatalf("read s21 migration: %v", err)
	}
	sql := string(raw)

	widthRe := regexp.MustCompile(`(?i)([a-z_]+)\s+VARCHAR\((\d+)\)`)
	declared := map[string]int{}
	for _, m := range widthRe.FindAllStringSubmatch(sql, -1) {
		w, err := strconv.Atoi(m[2])
		if err != nil {
			t.Fatalf("parse width in %q: %v", m[0], err)
		}
		declared[strings.ToLower(m[1])] = w
	}
	if len(declared) == 0 {
		t.Fatalf("no VARCHAR declarations parsed from %s — the parser or the file moved", s21MigrationPath)
	}

	// Use GORM's own naming strategy: a hand-rolled snake_case converter turns
	// "RunID" into "run__id", which silently skipped every ID column this test
	// exists to guard (caught by the plan's reverse-verification step).
	naming := schema.NamingStrategy{}
	for _, model := range []any{&magi.RuntimeInvocationModel{}, &magi.RuntimeInvocationAttemptModel{}} {
		rt := reflect.TypeOf(model).Elem()
		for i := 0; i < rt.NumField(); i++ {
			field := rt.Field(i)
			tag := field.Tag.Get("gorm")
			if tag == "" || strings.Contains(tag, "type:") {
				continue // non-varchar columns are covered by their own types
			}
			size := 0
			for _, part := range strings.Split(tag, ";") {
				if strings.HasPrefix(part, "size:") {
					size, _ = strconv.Atoi(strings.TrimPrefix(part, "size:"))
				}
			}
			if size == 0 {
				// Only columns with an explicit width constraint are comparable:
				// integers and TEXT columns carry no size tag.
				continue
			}
			column := naming.ColumnName("", field.Name)
			migWidth, ok := declared[column]
			if !ok {
				// A column the snapshot never mentions is AutoMigrate-only, but a
				// VARCHAR column the model declares and the snapshot omits is a
				// real gap for anyone provisioning from the snapshot.
				t.Errorf("%s.%s: model declares size %d but %s has no VARCHAR declaration for column %q",
					rt.Name(), field.Name, size, s21MigrationPath, column)
				continue
			}
			if migWidth < size {
				t.Errorf("%s.%s: migration declares VARCHAR(%d) but the model requires %d",
					rt.Name(), field.Name, migWidth, size)
			}
		}
	}
}
