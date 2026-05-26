package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oarkflow/condition/pkg/audit"
	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	if path != "" && path != ":memory:" && !strings.HasPrefix(path, "file:") {
		if dir := filepath.Dir(path); dir != "." && dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(30 * time.Minute)
	s := &SQLiteStore{db: db}
	if err := s.configure(context.Background(), path); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteStore) configure(ctx context.Context, path string) error {
	if err := s.db.PingContext(ctx); err != nil {
		return err
	}
	pragmas := []string{
		`PRAGMA foreign_keys = ON`,
		`PRAGMA busy_timeout = 5000`,
	}
	if path != ":memory:" {
		pragmas = append(pragmas,
			`PRAGMA journal_mode = WAL`,
			`PRAGMA synchronous = NORMAL`,
		)
	}
	for _, pragma := range pragmas {
		if _, err := s.db.ExecContext(ctx, pragma); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) SaveDefinition(ctx context.Context, record DefinitionRecord) error {
	if err := s.SaveDefinitionVersion(ctx, record); err != nil {
		return err
	}
	return s.ActivateDefinition(ctx, record.Name, record.Version, record.Environment)
}

func (s *SQLiteStore) SaveDefinitionVersion(ctx context.Context, record DefinitionRecord) error {
	payload, err := json.Marshal(record.Program)
	if err != nil {
		return err
	}
	if record.Environment == "" {
		record.Environment = "development"
	}
	record.TenantID = firstTenant(firstNonEmpty(record.TenantID, TenantFromContext(ctx)))
	metadata, _ := json.Marshal(record.Metadata)
	_, err = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO condition_definition_versions
(tenant_id, name, version, environment, source, source_path, digest, program_json, published_at, metadata_json)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.TenantID, record.Name, record.Version, record.Environment, record.Source, record.SourcePath, record.Digest, string(payload), record.PublishedAt.Format(time.RFC3339Nano), string(metadata))
	return err
}

func (s *SQLiteStore) GetDefinition(ctx context.Context, name string) (DefinitionRecord, error) {
	return s.GetActiveDefinition(ctx, name, "")
}

func (s *SQLiteStore) GetDefinitionVersion(ctx context.Context, name, version, environment string) (DefinitionRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT tenant_id, name, version, environment, source, source_path, digest, program_json, published_at, metadata_json FROM condition_definition_versions WHERE tenant_id = ? AND name = ? AND version = ? AND environment = ?`, TenantFromContext(ctx), name, version, firstEnv(environment))
	record, err := scanDefinition(row)
	if err == sql.ErrNoRows {
		return DefinitionRecord{}, fmt.Errorf("unknown definition %q version %q", name, version)
	}
	return record, err
}

func (s *SQLiteStore) ListDefinitions(ctx context.Context) ([]DefinitionRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT v.tenant_id, v.name, v.version, v.environment, v.source, v.source_path, v.digest, v.program_json, v.published_at, v.metadata_json
FROM condition_active_definitions a
JOIN condition_definition_versions v ON v.tenant_id = a.tenant_id AND v.name = a.name AND v.version = a.version AND v.environment = a.environment
WHERE a.tenant_id = ?
ORDER BY v.name`, TenantFromContext(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DefinitionRecord
	for rows.Next() {
		record, err := scanDefinition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) ListDefinitionVersions(ctx context.Context, name, environment string) ([]DefinitionRecord, error) {
	query := `SELECT tenant_id, name, version, environment, source, source_path, digest, program_json, published_at, metadata_json FROM condition_definition_versions WHERE tenant_id = ? AND name = ?`
	args := []any{TenantFromContext(ctx), name}
	if environment != "" {
		query += ` AND environment = ?`
		args = append(args, firstEnv(environment))
	}
	query += ` ORDER BY published_at`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DefinitionRecord
	for rows.Next() {
		record, err := scanDefinition(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) ActivateDefinition(ctx context.Context, name, version, environment string) error {
	if _, err := s.GetDefinitionVersion(ctx, name, version, environment); err != nil {
		return err
	}
	tenant := TenantFromContext(ctx)
	_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO condition_active_definitions (tenant_id, name, environment, version, activated_at)
VALUES (?, ?, ?, ?, ?)`,
		tenant, name, firstEnv(environment), version, time.Now().Format(time.RFC3339Nano))
	return err
}

func (s *SQLiteStore) GetActiveDefinition(ctx context.Context, name, environment string) (DefinitionRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT v.tenant_id, v.name, v.version, v.environment, v.source, v.source_path, v.digest, v.program_json, v.published_at, v.metadata_json
FROM condition_active_definitions a
JOIN condition_definition_versions v ON v.tenant_id = a.tenant_id AND v.name = a.name AND v.version = a.version AND v.environment = a.environment
WHERE a.tenant_id = ? AND a.name = ? AND a.environment = ?`, TenantFromContext(ctx), name, firstEnv(environment))
	record, err := scanDefinition(row)
	if err == sql.ErrNoRows {
		return DefinitionRecord{}, fmt.Errorf("unknown definition %q", name)
	}
	return record, err
}

func (s *SQLiteStore) SaveReport(ctx context.Context, report ReportRecord) error {
	payload, _ := json.Marshal(report.Payload)
	report.TenantID = firstTenant(firstNonEmpty(report.TenantID, TenantFromContext(ctx)))
	_, err := s.db.ExecContext(ctx, `INSERT INTO condition_reports (id, tenant_id, kind, definition, created_at, payload_json) VALUES (?, ?, ?, ?, ?, ?)`,
		report.ID, report.TenantID, report.Kind, report.Definition, report.CreatedAt.Format(time.RFC3339Nano), string(payload))
	return err
}

func (s *SQLiteStore) ListReports(ctx context.Context, kind string) ([]ReportRecord, error) {
	return s.ListReportsQuery(ctx, ListOptions{Kind: kind})
}

func (s *SQLiteStore) ListReportsQuery(ctx context.Context, opts ListOptions) ([]ReportRecord, error) {
	query := `SELECT id, tenant_id, kind, definition, created_at, payload_json FROM condition_reports`
	args := []any{}
	var where []string
	tenant := firstTenant(firstNonEmpty(opts.TenantID, TenantFromContext(ctx)))
	where = append(where, `tenant_id = ?`)
	args = append(args, tenant)
	if opts.Kind != "" {
		where = append(where, `kind = ?`)
		args = append(args, opts.Kind)
	}
	if opts.Definition != "" {
		where = append(where, `definition = ?`)
		args = append(args, opts.Definition)
	}
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, ` AND `)
	}
	query += ` ORDER BY created_at`
	if opts.Limit > 0 {
		query += fmt.Sprintf(` LIMIT %d`, opts.Limit)
	}
	if opts.Offset > 0 {
		query += fmt.Sprintf(` OFFSET %d`, opts.Offset)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReportRecord
	for rows.Next() {
		var report ReportRecord
		var payload, createdAt string
		if err := rows.Scan(&report.ID, &report.TenantID, &report.Kind, &report.Definition, &createdAt, &payload); err != nil {
			return nil, err
		}
		report.CreatedAt = parseTime(createdAt)
		if !inRange(report.CreatedAt, opts) {
			continue
		}
		_ = json.Unmarshal([]byte(payload), &report.Payload)
		out = append(out, report)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) listReportsOld(ctx context.Context, kind string) ([]ReportRecord, error) {
	query := `SELECT id, kind, definition, created_at, payload_json FROM condition_reports`
	args := []any{}
	if kind != "" {
		query += ` WHERE kind = ?`
		args = append(args, kind)
	}
	query += ` ORDER BY created_at`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReportRecord
	for rows.Next() {
		var report ReportRecord
		var payload, createdAt string
		if err := rows.Scan(&report.ID, &report.Kind, &report.Definition, &createdAt, &payload); err != nil {
			return nil, err
		}
		report.CreatedAt = parseTime(createdAt)
		_ = json.Unmarshal([]byte(payload), &report.Payload)
		out = append(out, report)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) AppendAudit(ctx context.Context, envelope audit.Envelope) error {
	payload, _ := json.Marshal(envelope)
	envelope.TenantID = firstTenant(firstNonEmpty(envelope.TenantID, TenantFromContext(ctx)))
	payload, _ = json.Marshal(envelope)
	_, err := s.db.ExecContext(ctx, `INSERT INTO condition_audits (id, tenant_id, hash, previous_hash, completed_at, payload_json) VALUES (?, ?, ?, ?, ?, ?)`,
		envelope.ID, envelope.TenantID, envelope.Hash, envelope.PreviousHash, envelope.CompletedAt.Format(time.RFC3339Nano), string(payload))
	return err
}

func (s *SQLiteStore) LastAuditHash(ctx context.Context) (string, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT hash FROM condition_audits WHERE tenant_id = ? ORDER BY completed_at DESC, rowid DESC LIMIT 1`, TenantFromContext(ctx)).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return hash, err
}

func (s *SQLiteStore) ListAudits(ctx context.Context) ([]audit.Envelope, error) {
	return s.ListAuditsQuery(ctx, ListOptions{})
}

func (s *SQLiteStore) ListAuditsQuery(ctx context.Context, opts ListOptions) ([]audit.Envelope, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload_json FROM condition_audits WHERE tenant_id = ? ORDER BY completed_at, rowid`, firstTenant(firstNonEmpty(opts.TenantID, TenantFromContext(ctx))))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []audit.Envelope
	for rows.Next() {
		var payload string
		var envelope audit.Envelope
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &envelope); err != nil {
			return nil, err
		}
		if (opts.Operation == "" || envelope.Operation == opts.Operation) &&
			(opts.Definition == "" || envelope.Definition == opts.Definition) &&
			(opts.Subject == "" || envelope.Subject == opts.Subject) &&
			inRange(envelope.CompletedAt, opts) {
			out = append(out, envelope)
		}
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return ApplyListOptions(out, opts, nil), nil
}

func (s *SQLiteStore) GetAudit(ctx context.Context, id string) (audit.Envelope, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload_json FROM condition_audits WHERE tenant_id = ? AND id = ?`, TenantFromContext(ctx), id).Scan(&payload)
	if err == sql.ErrNoRows {
		return audit.Envelope{}, fmt.Errorf("unknown audit %q", id)
	}
	if err != nil {
		return audit.Envelope{}, err
	}
	var envelope audit.Envelope
	return envelope, json.Unmarshal([]byte(payload), &envelope)
}

func (s *SQLiteStore) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS condition_definitions (
	tenant_id TEXT NOT NULL DEFAULT 'default',
	name TEXT PRIMARY KEY,
	version TEXT,
	environment TEXT,
	source TEXT,
	source_path TEXT,
	digest TEXT NOT NULL,
	program_json TEXT NOT NULL,
	published_at TIMESTAMP NOT NULL,
	metadata_json TEXT
);
CREATE TABLE IF NOT EXISTS condition_reports (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL DEFAULT 'default',
	kind TEXT NOT NULL,
	definition TEXT,
	created_at TIMESTAMP NOT NULL,
	payload_json TEXT
);
CREATE TABLE IF NOT EXISTS condition_audits (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL DEFAULT 'default',
	hash TEXT NOT NULL,
	previous_hash TEXT,
	completed_at TIMESTAMP NOT NULL,
	payload_json TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS condition_definition_versions (
	tenant_id TEXT NOT NULL DEFAULT 'default',
	name TEXT NOT NULL,
	version TEXT NOT NULL,
	environment TEXT NOT NULL,
	source TEXT,
	source_path TEXT,
	digest TEXT NOT NULL,
	program_json TEXT NOT NULL,
	published_at TEXT NOT NULL,
	metadata_json TEXT,
	PRIMARY KEY(tenant_id, name, version, environment)
);
CREATE TABLE IF NOT EXISTS condition_active_definitions (
	tenant_id TEXT NOT NULL DEFAULT 'default',
	name TEXT NOT NULL,
	environment TEXT NOT NULL,
	version TEXT NOT NULL,
	activated_at TEXT NOT NULL,
	PRIMARY KEY(tenant_id, name, environment)
);
CREATE TABLE IF NOT EXISTS condition_workflow_runs (
	id TEXT PRIMARY KEY,
	tenant_id TEXT NOT NULL DEFAULT 'default',
	definition TEXT,
	version TEXT,
	environment TEXT,
	workflow_id TEXT,
	stage TEXT,
	status TEXT,
	input_json TEXT,
	assignment_json TEXT,
	events_json TEXT,
	created_at TEXT,
	updated_at TEXT
);`)
	if err != nil {
		return err
	}
	for _, table := range []string{"condition_definitions", "condition_reports", "condition_audits", "condition_definition_versions", "condition_active_definitions", "condition_workflow_runs"} {
		if err := s.ensureColumn(ctx, table, "tenant_id", "TEXT NOT NULL DEFAULT 'default'"); err != nil {
			return err
		}
	}
	if err := s.ensureTenantPrimaryKeys(ctx); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStore) ensureTenantPrimaryKeys(ctx context.Context) error {
	checks := []struct {
		table string
		want  []string
		sql   string
		copy  string
	}{
		{
			table: "condition_definition_versions",
			want:  []string{"tenant_id", "name", "version", "environment"},
			sql: `CREATE TABLE condition_definition_versions_new (
tenant_id TEXT NOT NULL DEFAULT 'default',
name TEXT NOT NULL,
version TEXT NOT NULL,
environment TEXT NOT NULL,
source TEXT,
source_path TEXT,
digest TEXT NOT NULL,
program_json TEXT NOT NULL,
published_at TEXT NOT NULL,
metadata_json TEXT,
PRIMARY KEY(tenant_id, name, version, environment)
)`,
			copy: `INSERT OR REPLACE INTO condition_definition_versions_new
(tenant_id, name, version, environment, source, source_path, digest, program_json, published_at, metadata_json)
SELECT COALESCE(NULLIF(tenant_id, ''), 'default'), name, version, environment, source, source_path, digest, program_json, published_at, metadata_json
FROM condition_definition_versions`,
		},
		{
			table: "condition_active_definitions",
			want:  []string{"tenant_id", "name", "environment"},
			sql: `CREATE TABLE condition_active_definitions_new (
tenant_id TEXT NOT NULL DEFAULT 'default',
name TEXT NOT NULL,
environment TEXT NOT NULL,
version TEXT NOT NULL,
activated_at TEXT NOT NULL,
PRIMARY KEY(tenant_id, name, environment)
)`,
			copy: `INSERT OR REPLACE INTO condition_active_definitions_new
(tenant_id, name, environment, version, activated_at)
SELECT COALESCE(NULLIF(tenant_id, ''), 'default'), name, environment, version, activated_at
FROM condition_active_definitions`,
		},
	}
	for _, check := range checks {
		ok, err := s.primaryKeyMatches(ctx, check.table, check.want)
		if err != nil {
			return err
		}
		if ok {
			continue
		}
		if err := s.rebuildTable(ctx, check.table, check.sql, check.copy); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) primaryKeyMatches(ctx context.Context, table string, want []string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	got := make([]string, len(want))
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return false, err
		}
		if pk > 0 && pk <= len(got) {
			got[pk-1] = name
		}
	}
	if rows.Err() != nil {
		return false, rows.Err()
	}
	for i := range want {
		if got[i] != want[i] {
			return false, nil
		}
	}
	return true, nil
}

func (s *SQLiteStore) rebuildTable(ctx context.Context, table, createSQL, copySQL string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+table+`_new`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, createSQL); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, copySQL); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE `+table); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `ALTER TABLE `+table+`_new RENAME TO `+table); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) ensureColumn(ctx context.Context, table, column, decl string) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	if rows.Err() != nil {
		return rows.Err()
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, decl))
	return err
}

func (s *SQLiteStore) SaveWorkflowRun(ctx context.Context, run WorkflowRunRecord) error {
	input, _ := json.Marshal(run.Input)
	assignment, _ := json.Marshal(run.Assignment)
	events, _ := json.Marshal(run.Events)
	run.TenantID = firstTenant(firstNonEmpty(run.TenantID, TenantFromContext(ctx)))
	_, err := s.db.ExecContext(ctx, `INSERT INTO condition_workflow_runs
(id, tenant_id, definition, version, environment, workflow_id, stage, status, input_json, assignment_json, events_json, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET stage=excluded.stage, status=excluded.status, input_json=excluded.input_json,
assignment_json=excluded.assignment_json, events_json=excluded.events_json, updated_at=excluded.updated_at`,
		run.ID, run.TenantID, run.Definition, run.Version, run.Environment, run.WorkflowID, run.Stage, run.Status, string(input), string(assignment), string(events), run.CreatedAt.Format(time.RFC3339Nano), run.UpdatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *SQLiteStore) GetWorkflowRun(ctx context.Context, id string) (WorkflowRunRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, tenant_id, definition, version, environment, workflow_id, stage, status, input_json, assignment_json, events_json, created_at, updated_at FROM condition_workflow_runs WHERE tenant_id = ? AND id = ?`, TenantFromContext(ctx), id)
	run, err := scanWorkflowRun(row)
	if err == sql.ErrNoRows {
		return WorkflowRunRecord{}, fmt.Errorf("unknown workflow run %q", id)
	}
	return run, err
}

func (s *SQLiteStore) ListWorkflowRuns(ctx context.Context, opts ListOptions) ([]WorkflowRunRecord, error) {
	query := `SELECT id, tenant_id, definition, version, environment, workflow_id, stage, status, input_json, assignment_json, events_json, created_at, updated_at FROM condition_workflow_runs`
	var where []string
	var args []any
	where = append(where, `tenant_id = ?`)
	args = append(args, firstTenant(firstNonEmpty(opts.TenantID, TenantFromContext(ctx))))
	if opts.Definition != "" {
		where = append(where, `definition = ?`)
		args = append(args, opts.Definition)
	}
	if opts.Environment != "" {
		where = append(where, `environment = ?`)
		args = append(args, opts.Environment)
	}
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, ` AND `)
	}
	query += ` ORDER BY created_at`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkflowRunRecord
	for rows.Next() {
		run, err := scanWorkflowRun(rows)
		if err != nil {
			return nil, err
		}
		if inRange(run.UpdatedAt, opts) {
			out = append(out, run)
		}
	}
	return ApplyListOptions(out, opts, nil), rows.Err()
}

type definitionScanner interface {
	Scan(dest ...any) error
}

func scanDefinition(scanner definitionScanner) (DefinitionRecord, error) {
	var record DefinitionRecord
	var programJSON, metadataJSON, publishedAt string
	if err := scanner.Scan(&record.TenantID, &record.Name, &record.Version, &record.Environment, &record.Source, &record.SourcePath, &record.Digest, &programJSON, &publishedAt, &metadataJSON); err != nil {
		return DefinitionRecord{}, err
	}
	record.PublishedAt = parseTime(publishedAt)
	if err := json.Unmarshal([]byte(programJSON), &record.Program); err != nil {
		return DefinitionRecord{}, err
	}
	if metadataJSON != "" {
		_ = json.Unmarshal([]byte(metadataJSON), &record.Metadata)
	}
	if record.PublishedAt.IsZero() {
		record.PublishedAt = time.Now()
	}
	return record, nil
}

func scanWorkflowRun(scanner definitionScanner) (WorkflowRunRecord, error) {
	var run WorkflowRunRecord
	var inputJSON, assignmentJSON, eventsJSON, createdAt, updatedAt string
	if err := scanner.Scan(&run.ID, &run.TenantID, &run.Definition, &run.Version, &run.Environment, &run.WorkflowID, &run.Stage, &run.Status, &inputJSON, &assignmentJSON, &eventsJSON, &createdAt, &updatedAt); err != nil {
		return WorkflowRunRecord{}, err
	}
	_ = json.Unmarshal([]byte(inputJSON), &run.Input)
	_ = json.Unmarshal([]byte(assignmentJSON), &run.Assignment)
	_ = json.Unmarshal([]byte(eventsJSON), &run.Events)
	run.CreatedAt = parseTime(createdAt)
	run.UpdatedAt = parseTime(updatedAt)
	return run, nil
}

func parseTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Now()
	}
	return t
}
