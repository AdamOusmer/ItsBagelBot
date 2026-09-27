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
	m, err := openMigration(opts)
	if err != nil {
		return err
	}
	defer m.db.Close()
	defer m.printSummary()
	defer m.closeApply()
	if err := m.prepareApply(); err != nil {
		return err
	}
	if opts.restore != "" {
		return m.restore(ctx)
	}
	return m.scan(ctx)
}

func (o options) validate() error {
	if o.pageSize < 1 || o.pageSize > 1000 {
		return errors.New("invalid page size or timeout")
	}
	if o.timeout <= 0 || o.timeout > time.Minute {
		return errors.New("invalid page size or timeout")
	}
	if o.apply && o.backup == "" {
		return errors.New("--apply requires --backup with a new file path")
	}
	return nil
}

func openMigration(opts options) (*migration, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	dsn := os.Getenv(opts.dsnEnv)
	if dsn == "" {
		return nil, errors.New("the selected DSN environment variable is empty")
	}
	if opts.prefix == "" {
		opts.prefix = "bagel.rpc.modules"
	}
	db, err := ent.Open("mysql", dsn)
	if err != nil {
		return nil, errors.New("cannot open database reader")
	}
	return &migration{opts: opts, db: db}, nil
}

func (m *migration) printSummary() {
	fmt.Printf("scanned=%d changed=%d applied=%d conflicts=%d skipped=%d dry_run=%t\n", m.scanned, m.changed, m.applied, m.conflicts, m.skipped, !m.opts.apply)
}

func (m *migration) prepareApply() error {
	if !m.opts.apply {
		return nil
	}
	url := migrationRPCURL(m.opts.natsEnv)
	if url == "" {
		return errors.New("the selected NATS RPC endpoint is empty")
	}
	var err error
	m.nc, err = bus.Connect(url, "module-template-migration")
	if err != nil {
		return errors.New("cannot connect to modules RPC")
	}
	m.journal, err = createBackup(m.opts.backup)
	if err != nil {
		return errors.New("cannot create backup; it must be a new writable file")
	}
	return nil
}

func migrationRPCURL(fallbackEnv string) string {
	if leaf := os.Getenv("NATS_LEAF_URL"); leaf != "" {
		return leaf
	}
	return bus.RPCURL(os.Getenv(fallbackEnv))
}

func (m *migration) closeApply() {
	if m.journal != nil {
		_ = m.journal.Close()
	}
	if m.nc != nil {
		m.nc.Close()
	}
}

func createBackup(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
}

func (m *migration) scan(ctx context.Context) error {
	afterID, more := 0, true
	for more {
		var err error
		afterID, more, err = m.scanPage(ctx, afterID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *migration) scanPage(ctx context.Context, afterID int) (int, bool, error) {
	rows, err := m.readPage(ctx, afterID)
	if err != nil {
		return 0, false, err
	}
	if err := m.migratePage(ctx, rows); err != nil {
		return 0, false, err
	}
	if len(rows) < m.opts.pageSize {
		return 0, false, nil
	}
	return rows[len(rows)-1].ID, true, nil
}

func (m *migration) readPage(ctx context.Context, afterID int) ([]*ent.Modules, error) {
	query := m.db.Modules.Query().Where(modules.IDGT(afterID)).Order(ent.Asc(modules.FieldID)).Limit(m.opts.pageSize)
	if m.opts.userID != 0 {
		query.Where(modules.UserIDEQ(m.opts.userID))
	}
	qctx, cancel := context.WithTimeout(ctx, m.opts.timeout)
	defer cancel()
	rows, err := query.All(qctx)
	if err != nil {
		return nil, errors.New("database page read failed")
	}
	return rows, nil
}

func (m *migration) migratePage(ctx context.Context, rows []*ent.Modules) error {
	for _, row := range rows {
		if err := m.migrateRow(ctx, row); err != nil {
			return err
		}
	}
	return nil
}

func (m *migration) migrateRow(ctx context.Context, row *ent.Modules) error {
	m.scanned++
	partial, err := modulevars.MigrateConfig(row.Name, row.Configs)
	if err != nil {
		m.skipped++
		fmt.Printf("skip invalid config row=%d user=%d module=%s\n", row.ID, row.UserID, row.Name)
		return nil
	}
	if len(partial) == 0 {
		return nil
	}
	return m.patch(ctx, row, partial)
}

func (m *migration) patch(ctx context.Context, row *ent.Modules, partial map[string]codec.RawMessage) error {
	m.changed++
	logPatch(row, partial)
	if !m.opts.apply {
		return nil
	}
	entry, err := preparePatch(row, partial)
	if err != nil {
		return err
	}
	if err := m.record(entry); err != nil {
		return err
	}
	patch, err := codec.Marshal(partial)
	if err != nil {
		return errors.New("cannot encode migration patch")
	}
	reply, err := m.requestPatch(ctx, row, patch)
	return m.completePatch(ctx, row, entry, reply, err)
}

func logPatch(row *ent.Modules, partial map[string]codec.RawMessage) {
	keys := make([]string, 0, len(partial))
	for key := range partial {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fmt.Printf("change row=%d user=%d module=%s revision=%d keys=%s\n", row.ID, row.UserID, row.Name, row.Revision, strings.Join(keys, ","))
}

func preparePatch(row *ent.Modules, partial map[string]codec.RawMessage) (journalEntry, error) {
	var full map[string]codec.RawMessage
	if err := codec.Unmarshal(row.Configs, &full); err != nil {
		return journalEntry{}, errors.New("cannot prepare a complete backup")
	}
	if full == nil {
		return journalEntry{}, errors.New("cannot prepare a complete backup")
	}
	before, err := replaceFields(full, partial)
	if err != nil {
		return journalEntry{}, err
	}
	rev, _ := codec.Marshal(row.Revision + 1)
	full["__rev"] = rev
	after, err := codec.Marshal(full)
	if err != nil {
		return journalEntry{}, errors.New("cannot encode backup")
	}
	return journalEntry{Kind: "prepared", At: time.Now().UTC(), RowID: row.ID, UserID: row.UserID, Module: row.Name,
		BeforeRevision: row.Revision, AfterRevision: row.Revision + 1, IsEnabled: row.IsEnabled,
		BeforeConfigs: row.Configs, AfterConfigs: after, Before: before, After: partial}, nil
}

func replaceFields(full, partial map[string]codec.RawMessage) (map[string]codec.RawMessage, error) {
	before := make(map[string]codec.RawMessage, len(partial))
	for key, value := range partial {
		old, present := full[key]
		if !present {
			return nil, errors.New("migration attempted to change a missing key")
		}
		before[key] = old
		full[key] = value
	}
	return before, nil
}

func (m *migration) requestPatch(ctx context.Context, row *ent.Modules, patch []byte) (modulesrpc.DashboardReply, error) {
	qctx, cancel := context.WithTimeout(ctx, m.opts.timeout)
	defer cancel()
	return bus.RequestJSON[modulesrpc.DashboardReply](qctx, m.nc, m.opts.prefix+".patch-existing", modulesrpc.DashboardRequest{
		UserID: strconv.FormatUint(row.UserID, 10), Name: row.Name, IsEnabled: row.IsEnabled, Configs: patch, ExpectedRev: &row.Revision, ExpectedID: &row.ID})
}

func (m *migration) completePatch(ctx context.Context, row *ent.Modules, entry journalEntry, reply modulesrpc.DashboardReply, rpcErr error) error {
	entry.BeforeConfigs, entry.AfterConfigs, entry.Before, entry.After = nil, nil, nil, nil
	entry.At = time.Now().UTC()
	if !patchOutcomeCertain(row, reply, rpcErr) {
		return m.recordUncertain(entry)
	}
	entry.AfterRevision = reply.Rev
	if reply.Conflict {
		entry.Kind = "conflict"
		m.conflicts++
		m.logConflict(ctx, row)
	} else {
		entry.Kind = "applied"
		m.applied++
	}
	return m.record(entry)
}

func patchOutcomeCertain(row *ent.Modules, reply modulesrpc.DashboardReply, rpcErr error) bool {
	if rpcErr != nil {
		return false
	}
	if reply.Error != "" {
		return false
	}
	if reply.Conflict {
		return true
	}
	return reply.Rev == row.Revision+1
}

func (m *migration) recordUncertain(entry journalEntry) error {
	entry.Kind = "uncertain"
	if err := m.record(entry); err != nil {
		return err
	}
	return errors.New("patch RPC failed; its outcome is uncertain, inspect the row before retrying")
}

func (m *migration) logConflict(ctx context.Context, row *ent.Modules) {
	fresh, err := m.readRow(ctx, row.ID)
	if err != nil {
		fmt.Printf("skip conflict row=%d user=%d module=%s refetch_failed=true\n", row.ID, row.UserID, row.Name)
		return
	}
	fmt.Printf("skip conflict row=%d user=%d module=%s current_revision=%d\n", row.ID, row.UserID, row.Name, fresh.Revision)
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
	journal := restoreJournal{decoder: codec.NewDecoder(input)}
	for err == nil {
		err = m.restoreNext(ctx, &journal)
	}
	return m.finishRestore(journal, err)
}

type restoreJournal struct {
	decoder  codec.Decoder
	prepared *journalEntry
}

type operationIdentity struct {
	rowID    int
	userID   uint64
	module   string
	revision int
}

func (e journalEntry) identity() operationIdentity {
	return operationIdentity{rowID: e.RowID, userID: e.UserID, module: e.Module, revision: e.BeforeRevision}
}

func (j *restoreJournal) next() (*journalEntry, error) {
	var entry journalEntry
	if err := j.decoder.Decode(&entry); err != nil {
		return nil, restoreDecodeError(err)
	}
	if entry.Kind == "prepared" {
		return nil, j.prepare(entry)
	}
	return j.complete(entry)
}

func restoreDecodeError(err error) error {
	if errors.Is(err, io.EOF) {
		return err
	}
	return errors.New("restore journal is incomplete or invalid")
}

func (j *restoreJournal) prepare(entry journalEntry) error {
	if j.prepared != nil {
		return errors.New("restore journal has overlapping operations")
	}
	j.prepared = &entry
	return nil
}

func (j *restoreJournal) complete(entry journalEntry) (*journalEntry, error) {
	if j.prepared == nil {
		return nil, errors.New("restore journal outcome does not match its backup")
	}
	if entry.identity() != j.prepared.identity() {
		return nil, errors.New("restore journal outcome does not match its backup")
	}
	original := j.prepared
	j.prepared = nil
	if entry.Kind != "applied" {
		return nil, nil
	}
	return original, nil
}

func (m *migration) restoreNext(ctx context.Context, journal *restoreJournal) error {
	original, err := journal.next()
	if err != nil {
		return err
	}
	if original == nil {
		return nil
	}
	return m.restoreEntry(ctx, *original)
}

func (m *migration) finishRestore(journal restoreJournal, err error) error {
	if !errors.Is(err, io.EOF) {
		return err
	}
	if journal.prepared != nil {
		m.skipped++
		fmt.Printf("skip unconfirmed backup row=%d\n", journal.prepared.RowID)
	}
	return nil
}

func (m *migration) restoreEntry(ctx context.Context, original journalEntry) error {
	if m.opts.userID != 0 && original.UserID != m.opts.userID {
		return nil
	}
	m.scanned++
	row, err := m.readRestoreRow(ctx, original.RowID)
	if err != nil {
		return err
	}
	if row == nil {
		return nil
	}
	if !original.canRestore(row) {
		m.skipped++
		fmt.Printf("skip edited restore row=%d user=%d module=%s\n", row.ID, row.UserID, row.Name)
		return nil
	}
	return m.patch(ctx, row, original.Before)
}

func (m *migration) readRestoreRow(ctx context.Context, id int) (*ent.Modules, error) {
	row, err := m.readRow(ctx, id)
	if err == nil {
		return row, nil
	}
	if !ent.IsNotFound(err) {
		return nil, errors.New("restore database read failed")
	}
	m.skipped++
	fmt.Printf("skip missing restore row=%d\n", id)
	return nil, nil
}

func (e journalEntry) canRestore(row *ent.Modules) bool {
	if row.UserID != e.UserID || row.Name != e.Module {
		return false
	}
	if len(e.Before) == 0 {
		return false
	}
	if !sameKeys(e.Before, e.After) {
		return false
	}
	var current map[string]codec.RawMessage
	if codec.Unmarshal(row.Configs, &current) != nil {
		return false
	}
	return sameChangedValues(current, e.After)
}

func sameChangedValues(current, expected map[string]codec.RawMessage) bool {
	for key, want := range expected {
		got, found := current[key]
		if !found {
			return false
		}
		if !equivalentJSON(got, want) {
			return false
		}
	}
	return true
}

func equivalentJSON(got, want []byte) bool {
	a, err := comparisonValue(got)
	if err != nil {
		return false
	}
	b, err := comparisonValue(want)
	if err != nil {
		return false
	}
	return reflect.DeepEqual(a, b)
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
