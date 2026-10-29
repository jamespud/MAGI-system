package magi_test

import (
	"os"
	"strings"
	"testing"
)

func applyAuthorityMigration(t *testing.T, f *authoritySQLFixture, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read authority migration %s: %v", path, err)
	}
	for _, stmt := range strings.Split(string(raw), ";") {
		var lines []string
		for _, line := range strings.Split(stmt, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "--") {
				lines = append(lines, line)
			}
		}
		stmt = strings.TrimSpace(strings.Join(lines, "\n"))
		if stmt != "" {
			if err := f.db.Exec(stmt).Error; err != nil {
				t.Fatalf("apply authority migration %s: %v\n%s", path, err, stmt)
			}
		}
	}
}
