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
