// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// module-template-migrate previews or patches saved module reply templates. Database
// access is SELECT-only; all writes use the modules service's revision guard.
package main

import (
	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/modules"
	"ItsBagelBot/internal/domain/modulevars"
	modulesrpc "ItsBagelBot/internal/domain/rpc/modules"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/nats-io/nats.go"
)

type options struct {
	apply                   bool
	dsnEnv, natsEnv, prefix string
	backup, restore         string
	userID                  uint64
	pageSize                int
	timeout                 time.Duration
}

type journalEntry struct {
	Kind           string                      `json:"kind"`
	At             time.Time                   `json:"at"`
	RowID          int                         `json:"row_id"`
	UserID         uint64                      `json:"user_id"`
	Module         string                      `json:"module"`
	BeforeRevision int                         `json:"before_revision"`
	AfterRevision  int                         `json:"after_revision"`
	IsEnabled      bool                        `json:"is_enabled"`
	BeforeConfigs  codec.RawMessage            `json:"before_configs,omitempty"`
	AfterConfigs   codec.RawMessage            `json:"after_configs,omitempty"`
	Before         map[string]codec.RawMessage `json:"before,omitempty"`
	After          map[string]codec.RawMessage `json:"after,omitempty"`
}

type migration struct {
	opts                      options
	db                        *ent.Client
	nc                        *nats.Conn
	journal                   *os.File
	scanned, changed, applied int
	conflicts, skipped        int
}

func main() {
	var opts options
	flag.BoolVar(&opts.apply, "apply", false, "apply changes through modules.patch-existing; otherwise preview only")
	flag.StringVar(&opts.dsnEnv, "dsn-env", "MODULES_MIGRATION_DSN", "environment variable containing the read-only MySQL DSN")
	flag.StringVar(&opts.natsEnv, "nats-url-env", "NATS_URL", "environment variable containing the NATS RPC fallback URL")
	flag.StringVar(&opts.prefix, "subject-prefix", os.Getenv("NATS_MODULES_SUBJECT_PREFIX"), "modules RPC subject prefix; defaults to bagel.rpc.modules")
	flag.StringVar(&opts.backup, "backup", "", "new exclusive 0600 JSONL journal; required with --apply")
	flag.StringVar(&opts.restore, "restore", "", "preview or restore changed keys from a migration journal")
	flag.Uint64Var(&opts.userID, "user-id", 0, "restrict to one Twitch user ID; zero scans all users")
	flag.IntVar(&opts.pageSize, "page-size", 200, "database page size, between 1 and 1000")
	flag.DurationVar(&opts.timeout, "timeout", 10*time.Second, "timeout for each database read and RPC")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "module-template-migrate accepts flags only")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, opts); err != nil {
		// Errors are deliberately stage-only: upstream errors may include
		// credentials, SQL details, or saved template contents.
		fmt.Fprintln(os.Stderr, "module-template-migrate:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, opts options) error {
	if opts.pageSize < 1 || opts.pageSize > 1000 || opts.timeout <= 0 || opts.timeout > time.Minute {
		return errors.New("invalid page size or timeout")
	}
	if opts.apply && opts.backup == "" {
		return errors.New("--apply requires --backup with a new file path")
	}
	dsn := os.Getenv(opts.dsnEnv)
	if dsn == "" {
		return errors.New("the selected DSN environment variable is empty")
	}
	if opts.prefix == "" {
		opts.prefix = "bagel.rpc.modules"
	}
	db, err := ent.Open("mysql", dsn)
	if err != nil {
		return errors.New("cannot open database reader")
	}
	defer db.Close()
	m := migration{opts: opts, db: db}
	defer func() {
		fmt.Printf("scanned=%d changed=%d applied=%d conflicts=%d skipped=%d dry_run=%t\n", m.scanned, m.changed, m.applied, m.conflicts, m.skipped, !opts.apply)
	}()
	if opts.apply {
		url := bus.RPCURL(os.Getenv(opts.natsEnv))
		if leaf := os.Getenv("NATS_LEAF_URL"); leaf != "" {
			url = leaf
		}
		if url == "" {
			return errors.New("the selected NATS RPC endpoint is empty")
		}
		m.nc, err = bus.Connect(url, "module-template-migration")
		if err != nil {
			return errors.New("cannot connect to modules RPC")
		}
		defer m.nc.Close()
		m.journal, err = createBackup(opts.backup)
		if err != nil {
			return errors.New("cannot create backup; it must be a new writable file")
		}
		defer m.journal.Close()
	}
	if opts.restore != "" {
		return m.restore(ctx)
	}
	return m.scan(ctx)
}

func createBackup(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
}

func (m *migration) scan(ctx context.Context) error {
	for afterID := 0; ; {
		query := m.db.Modules.Query().Where(modules.IDGT(afterID)).Order(ent.Asc(modules.FieldID)).Limit(m.opts.pageSize)
		if m.opts.userID != 0 {
			query.Where(modules.UserIDEQ(m.opts.userID))
		}
		qctx, cancel := context.WithTimeout(ctx, m.opts.timeout)
		rows, err := query.All(qctx)
		cancel()
		if err != nil {
			return errors.New("database page read failed")
		}
		for _, row := range rows {
			m.scanned++
			partial, err := modulevars.MigrateConfig(row.Name, row.Configs)
			if err != nil {
				m.skipped++
				fmt.Printf("skip invalid config row=%d user=%d module=%s\n", row.ID, row.UserID, row.Name)
				continue
			}
			if len(partial) > 0 {
				if err := m.patch(ctx, row, partial); err != nil {
					return err
				}
			}
		}
		if len(rows) < m.opts.pageSize {
			return nil
		}
		afterID = rows[len(rows)-1].ID
	}
}

func (m *migration) patch(ctx context.Context, row *ent.Modules, partial map[string]codec.RawMessage) error {
	m.changed++
	keys := make([]string, 0, len(partial))
	for key := range partial {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fmt.Printf("change row=%d user=%d module=%s revision=%d keys=%s\n", row.ID, row.UserID, row.Name, row.Revision, strings.Join(keys, ","))
	if !m.opts.apply {
		return nil
	}
	var full map[string]codec.RawMessage
	if err := codec.Unmarshal(row.Configs, &full); err != nil || full == nil {
		return errors.New("cannot prepare a complete backup")
	}
	before := make(map[string]codec.RawMessage, len(partial))
	for key, value := range partial {
		old, present := full[key]
		if !present {
			return errors.New("migration attempted to change a missing key")
		}
		before[key] = old
		full[key] = value
	}
	rev, _ := codec.Marshal(row.Revision + 1)
	full["__rev"] = rev
	after, err := codec.Marshal(full)
	if err != nil {
		return errors.New("cannot encode backup")
	}
	entry := journalEntry{Kind: "prepared", At: time.Now().UTC(), RowID: row.ID, UserID: row.UserID, Module: row.Name,
		BeforeRevision: row.Revision, AfterRevision: row.Revision + 1, IsEnabled: row.IsEnabled,
		BeforeConfigs: row.Configs, AfterConfigs: after, Before: before, After: partial}
	if err := m.record(entry); err != nil {
		return err
	}
	patch, err := codec.Marshal(partial)
	if err != nil {
		return errors.New("cannot encode migration patch")
	}
	qctx, cancel := context.WithTimeout(ctx, m.opts.timeout)
	reply, err := bus.RequestJSON[modulesrpc.DashboardReply](qctx, m.nc, m.opts.prefix+".patch-existing", modulesrpc.DashboardRequest{
		UserID: strconv.FormatUint(row.UserID, 10), Name: row.Name, IsEnabled: row.IsEnabled, Configs: patch, ExpectedRev: &row.Revision, ExpectedID: &row.ID})
	cancel()
	entry.BeforeConfigs, entry.AfterConfigs, entry.Before, entry.After = nil, nil, nil, nil
	entry.At = time.Now().UTC()
	if err != nil || reply.Error != "" || !reply.Conflict && reply.Rev != row.Revision+1 {
		entry.Kind = "uncertain"
		if journalErr := m.record(entry); journalErr != nil {
			return journalErr
		}
		return errors.New("patch RPC failed; its outcome is uncertain, inspect the row before retrying")
	}
	entry.AfterRevision = reply.Rev
	if reply.Conflict {
		entry.Kind = "conflict"
		m.conflicts++
		fresh, readErr := m.readRow(ctx, row.ID)
		if readErr == nil {
			fmt.Printf("skip conflict row=%d user=%d module=%s current_revision=%d\n", row.ID, row.UserID, row.Name, fresh.Revision)
		} else {
			fmt.Printf("skip conflict row=%d user=%d module=%s refetch_failed=true\n", row.ID, row.UserID, row.Name)
		}
	} else {
		entry.Kind = "applied"
		m.applied++
	}
	return m.record(entry)
}

func (m *migration) record(entry journalEntry) error {
	if err := codec.NewEncoder(m.journal).Encode(entry); err != nil {
		return errors.New("backup write failed; stopped before further patches")
	}
	if err := m.journal.Sync(); err != nil {
		return errors.New("backup fsync failed; stopped before further patches")
	}
	return nil
}

func (m *migration) readRow(ctx context.Context, id int) (*ent.Modules, error) {
	qctx, cancel := context.WithTimeout(ctx, m.opts.timeout)
	defer cancel()
	return m.db.Modules.Get(qctx, id)
}

// restore only reverts fields whose values still match the recorded migration.
// It leaves later unrelated config edits and the current enable state intact.
func (m *migration) restore(ctx context.Context) error {
	input, err := os.Open(m.opts.restore)
	if err != nil {
		return errors.New("cannot read restore journal")
	}
	defer input.Close()
	decoder := codec.NewDecoder(input)
	var prepared *journalEntry
	for {
		var entry journalEntry
		if err := decoder.Decode(&entry); err != nil {
			if errors.Is(err, io.EOF) {
				if prepared != nil {
					m.skipped++
					fmt.Printf("skip unconfirmed backup row=%d\n", prepared.RowID)
				}
				return nil
			}
			return errors.New("restore journal is incomplete or invalid")
		}
		if entry.Kind == "prepared" {
			if prepared != nil {
				return errors.New("restore journal has overlapping operations")
			}
			prepared = &entry
			continue
		}
		if prepared == nil || entry.RowID != prepared.RowID || entry.UserID != prepared.UserID || entry.Module != prepared.Module || entry.BeforeRevision != prepared.BeforeRevision {
			return errors.New("restore journal outcome does not match its backup")
		}
		original := *prepared
		prepared = nil
		if entry.Kind != "applied" || m.opts.userID != 0 && original.UserID != m.opts.userID {
			continue
		}
		m.scanned++
		row, err := m.readRow(ctx, original.RowID)
		if err != nil {
			if !ent.IsNotFound(err) {
				return errors.New("restore database read failed")
			}
			m.skipped++
			fmt.Printf("skip missing restore row=%d\n", original.RowID)
			continue
		}
		var current map[string]codec.RawMessage
		if row.UserID != original.UserID || row.Name != original.Module || codec.Unmarshal(row.Configs, &current) != nil || !sameChangedValues(current, original.After) || !sameKeys(original.Before, original.After) || len(original.Before) == 0 {
			m.skipped++
			fmt.Printf("skip edited restore row=%d user=%d module=%s\n", row.ID, row.UserID, row.Name)
			continue
		}
		if err := m.patch(ctx, row, original.Before); err != nil {
			return err
		}
	}
}

func sameChangedValues(current, expected map[string]codec.RawMessage) bool {
	for key, want := range expected {
		got, found := current[key]
		a, errA := comparisonValue(got)
		b, errB := comparisonValue(want)
		if !found || errA != nil || errB != nil || !reflect.DeepEqual(a, b) {
			return false
		}
	}
	return true
}

func comparisonValue(raw []byte) (any, error) {
	decoder := codec.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("invalid comparison value")
	}
	return value, nil
}

func sameKeys(a, b map[string]codec.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if _, found := b[key]; !found {
			return false
		}
	}
	return true
}
