// This file is compiled against the pinned pre-T1 backend, never the current
// adapter. It exercises that binary's real unfenced status and cleanup methods.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"

	magi "github.com/jamespud/magi/backend/adapter"
	"github.com/jamespud/magi/backend/domain/entity"
	"github.com/jamespud/magi/backend/domain/port"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func main() {
	denied := os.Getenv("T6_PROBE_MODE") == "denied"
	db, err := gorm.Open(mysql.Open(os.Getenv("T6_PROBE_DSN")), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		if denied {
			fmt.Println("CONNECT_DENIED")
			return
		}
		fmt.Fprintln(os.Stderr, "legacy positive control could not connect")
		os.Exit(1)
	}
	pool, err := db.DB()
	if err != nil {
		os.Exit(1)
	}
	defer pool.Close()
	pool.SetMaxOpenConns(1)
	var id uint64
	if err = db.Raw("SELECT CONNECTION_ID()").Scan(&id).Error; err != nil {
		os.Exit(1)
	}
	fmt.Printf("READY %d\n", id)
	// Retain the authenticated connection until the parent revokes/locks/kills.
	if _, err = bufio.NewReader(os.Stdin).ReadString('\n'); err != nil {
		os.Exit(1)
	}
	repo := magi.NewRepository(db)
	statusErr := repo.CaseRepo().UpdateStatus(context.Background(), os.Getenv("T6_PROBE_CASE"), entity.CaseStatusFailed)
	cleanupErr := repo.(port.ArtifactCleaner).CleanupCaseArtifacts(context.Background(), os.Getenv("T6_PROBE_CASE"))
	if denied {
		if statusErr == nil || cleanupErr == nil {
			fmt.Fprintln(os.Stderr, "old binary retained write authority")
			os.Exit(1)
		}
		fmt.Println("STATUS_DENIED CLEANUP_DENIED")
	} else {
		if statusErr != nil || cleanupErr != nil {
			fmt.Fprintln(os.Stderr, "legacy positive control failed")
			os.Exit(1)
		}
		fmt.Println("STATUS_WRITTEN CLEANUP_WRITTEN")
	}
}
