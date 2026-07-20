package bootstrap

import (
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// MysqlDialector builds the mysql dialector used by provideDB. Exported so the
// config (notably DefaultStringSize) can be guarded by tests.
//
// DefaultStringSize is set to 191 because the mysql driver otherwise emits
// `longtext` for string fields that have no size, are not a primary key, and
// -- due to a driver quirk -- use `gorm:"uniqueIndex"` (the hasIndex check in
// getSchemaStringType looks for TagSettings["INDEX"]/["UNIQUE"], not
// ["UNIQUEINDEX"]). A `longtext` column with an index triggers MySQL error
// 1170 (BLOB/TEXT column used in key specification without a key length).
// 191 is the utf8mb4-safe indexable size.
func MysqlDialector(dsn string) gorm.Dialector {
	return mysql.New(mysql.Config{
		DSN:                       dsn,
		SkipInitializeWithVersion: true,
		DefaultStringSize:         191,
	})
}
