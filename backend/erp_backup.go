package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

func backupERPDatabase(source, destination string) error {
	source, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	info, err := os.Stat(source)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("ERP source must be an existing database file")
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		return errors.New("ERP backup requires a new destination path")
	}
	dir, err := os.MkdirTemp(filepath.Dir(destination), ".erp-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	uriPath := filepath.ToSlash(source)
	if len(filepath.VolumeName(source)) == 2 {
		uriPath = "/" + uriPath
	}
	readOnly := url.URL{Scheme: "file", Path: uriPath, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", readOnly.String())
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout=10000"); err != nil {
		return err
	}
	var marker int
	err = db.QueryRowContext(ctx, "SELECT count(*) FROM erp_repository_schema WHERE version >= 1").Scan(&marker)
	if err != nil || marker != 1 {
		return errors.New("Source is missing a valid ERP repository schema marker")
	}
	staged := filepath.Join(dir, "snapshot.sqlite")
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", staged); err != nil {
		return fmt.Errorf("create ERP snapshot: %w", err)
	}
	if err := os.Chmod(staged, 0600); err != nil {
		return err
	}
	file, err := os.OpenFile(staged, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(syncErr, closeErr); err != nil {
		return err
	}
	// Publish on the same filesystem. The link fails if a destination appeared
	// during the snapshot, so the command cannot replace an existing file.
	return os.Link(staged, destination)
}
