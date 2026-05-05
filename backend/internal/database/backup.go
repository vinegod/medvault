package database

import (
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BackupConfig is the runtime knob set for the scheduled-backup goroutine.
// Zero Dir disables backups entirely.
type BackupConfig struct {
	Dir        string        // empty → backups disabled
	RetainDays int           // 0 → keep forever
	Hour       int           // 0..23, local time
	Interval   time.Duration // optional override of the daily cadence; 0 = 24h
}

// StartBackupScheduler launches a goroutine that, every Interval (default
// 24h), runs a SQLite consistent snapshot via VACUUM INTO, gzips it, and
// writes it under cfg.Dir. Old files past RetainDays are pruned.
//
// Returns immediately; the returned cancel function stops the scheduler.
// On a disabled configuration (cfg.Dir == "") this is a no-op that returns
// a no-op cancel — callers don't need to special-case "is it enabled".
func StartBackupScheduler(db *sql.DB, cfg BackupConfig) context.CancelFunc {
	if cfg.Dir == "" {
		slog.Info("backup scheduler disabled (BACKUP_DIR not set)")
		return func() {}
	}

	if err := os.MkdirAll(cfg.Dir, 0o750); err != nil {
		slog.Error("backup: create dir failed; scheduler not started",
			"dir", cfg.Dir, "err", err)
		return func() {}
	}

	interval := cfg.Interval
	if interval == 0 {
		interval = 24 * time.Hour
	}

	ctx, cancel := context.WithCancel(context.Background())
	go runBackupLoop(ctx, db, cfg, interval)
	return cancel
}

func runBackupLoop(ctx context.Context, db *sql.DB, cfg BackupConfig, interval time.Duration) {
	// First run is delayed until the next configured Hour, so a daily
	// schedule lands at 03:00 (or whatever) instead of "right now after
	// process restart, then again 24h later."
	delay := untilNextHour(time.Now(), cfg.Hour)
	timer := time.NewTimer(delay)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := runOnce(ctx, db, cfg); err != nil {
				slog.Error("backup run failed", "err", err)
			}
			timer.Reset(interval)
		}
	}
}

// runOnce takes one snapshot and prunes old files.
func runOnce(ctx context.Context, db *sql.DB, cfg BackupConfig) error {
	stamp := time.Now().UTC().Format("20060102-150405")
	tmpPath := filepath.Join(cfg.Dir, "snapshot-"+stamp+".tmp.db")
	finalPath := filepath.Join(cfg.Dir, "medvault-"+stamp+".db.gz")

	// VACUUM INTO writes a consistent snapshot of the live database to
	// tmpPath. Unlike a raw file copy, this works even with WAL writers
	// active — SQLite serialises the snapshot through its own locking.
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", tmpPath); err != nil {
		return fmt.Errorf("vacuum into: %w", err)
	}
	defer func() {
		// Remove the uncompressed temp file even on success — we keep the
		// gzipped final, not the raw .db.
		if err := os.Remove(tmpPath); err != nil && !os.IsNotExist(err) {
			slog.Warn("backup: remove temp", "path", tmpPath, "err", err)
		}
	}()

	if err := gzipFile(tmpPath, finalPath); err != nil {
		// Try to clean up a half-written final file.
		_ = os.Remove(finalPath)
		return fmt.Errorf("gzip: %w", err)
	}

	if cfg.RetainDays > 0 {
		if err := pruneOldBackups(cfg.Dir, cfg.RetainDays); err != nil {
			// Logged but not fatal — the new backup is already on disk.
			slog.Warn("backup: prune", "err", err)
		}
	}

	slog.Info("backup written", "path", finalPath)
	return nil
}

func gzipFile(src, dst string) error {
	in, err := os.Open(src) //nolint:gosec // src is a path we just wrote
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer in.Close() //nolint:errcheck // best-effort close on read-only file

	out, err := os.Create(dst) //nolint:gosec // dst is constructed from cfg
	if err != nil {
		return fmt.Errorf("create dst: %w", err)
	}
	defer out.Close() //nolint:errcheck // gz.Close() flushes; this is double-close

	gz := gzip.NewWriter(out)
	if _, err := io.Copy(gz, in); err != nil {
		return fmt.Errorf("copy: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("close gzip: %w", err)
	}
	return nil
}

// pruneOldBackups deletes any medvault-*.db.gz file in dir whose mtime is
// older than retainDays. Runs after each successful snapshot so the dir
// settles around N=retainDays files at steady state.
func pruneOldBackups(dir string, retainDays int) error {
	cutoff := time.Now().Add(-time.Duration(retainDays) * 24 * time.Hour)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read dir: %w", err)
	}
	type victim struct {
		path string
		mod  time.Time
	}
	var victims []victim
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "medvault-") || !strings.HasSuffix(name, ".db.gz") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			victims = append(victims, victim{filepath.Join(dir, name), info.ModTime()})
		}
	}
	// Sort newest-first so a buggy cutoff at least preserves recent files
	// when iteration is interrupted by a permission error mid-loop.
	sort.Slice(victims, func(i, j int) bool { return victims[i].mod.After(victims[j].mod) })
	for _, v := range victims {
		if err := os.Remove(v.path); err != nil {
			slog.Warn("backup: prune remove", "path", v.path, "err", err)
		}
	}
	return nil
}

// untilNextHour returns the duration from now to the next occurrence of
// hour:00:00 in local time. If hour is the current hour past 0 minutes,
// it skips ahead one day.
func untilNextHour(now time.Time, hour int) time.Duration {
	if hour < 0 || hour > 23 {
		hour = 3 // safe default
	}
	target := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
	if !target.After(now) {
		target = target.Add(24 * time.Hour)
	}
	return target.Sub(now)
}
