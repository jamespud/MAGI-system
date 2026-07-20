package bootstrap_test

import (
	"strings"
	"sync"
	"testing"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/bootstrap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm/schema"
)

// TestMysqlDialector_ResolutionCaseIDIsVarchar guards against the GORM mysql
// driver quirk where a string field with `gorm:"uniqueIndex"` (and no size)
// falls back to `longtext` when DefaultStringSize is unset -- which then fails
// MySQL error 1170 (BLOB/TEXT column used in key without length). The dialector
// returned by bootstrap must set DefaultStringSize so CaseID is varchar.
func TestMysqlDialector_ResolutionCaseIDIsVarchar(t *testing.T) {
	d := bootstrap.MysqlDialector("magi:magi123@tcp(127.0.0.1:3307)/magi?charset=utf8mb4")
	mysqlD, ok := d.(*mysql.Dialector)
	if !ok {
		t.Fatalf("expected *mysql.Dialector, got %T", d)
	}
	if mysqlD.DefaultStringSize == 0 {
		t.Fatal("DefaultStringSize must be set (>0) to avoid longtext for indexed string fields")
	}

	s, err := schema.Parse(&magi.ResolutionModel{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, f := range s.Fields {
		if f.Name == "CaseID" {
			ty := d.DataTypeOf(f)
			if !strings.HasPrefix(ty, "varchar") {
				t.Fatalf("ResolutionModel.CaseID must be varchar (indexable), got %q -- set DefaultStringSize on the mysql dialector", ty)
			}
			return
		}
	}
	t.Fatal("CaseID field not found on ResolutionModel")
}
