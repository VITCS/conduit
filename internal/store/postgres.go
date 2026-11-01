package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"conduit/internal/model"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib" // register the "pgx" database/sql driver
)

// pgNotifyChannel is the Postgres LISTEN/NOTIFY channel for cross-instance events.
const pgNotifyChannel = "conduit_events"

// Pg is a Postgres-backed Store. Workflows and runs are stored as JSONB with a
// few promoted columns for ordering and filtering. Live events fan out through a
// local broker and, via LISTEN/NOTIFY, across every server instance.
type Pg struct {
	db     *sql.DB
	dsn    string
	broker *Broker
}

const schema = `
CREATE TABLE IF NOT EXISTS projects (
    id         text PRIMARY KEY,
    name       text NOT NULL,
    created_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS workflows (
    id         text PRIMARY KEY,
    project_id text NOT NULL DEFAULT 'default',
    name       text NOT NULL,
    data       jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE workflows ADD COLUMN IF NOT EXISTS project_id text NOT NULL DEFAULT 'default';
CREATE TABLE IF NOT EXISTS runs (
    id          text PRIMARY KEY,
    workflow_id text NOT NULL,
    project_id  text NOT NULL DEFAULT 'default',
    status      text NOT NULL,
    data        jsonb NOT NULL,
    created_at  timestamptz NOT NULL,
    finished_at timestamptz
);
ALTER TABLE runs ADD COLUMN IF NOT EXISTS project_id text NOT NULL DEFAULT 'default';
ALTER TABLE runs ADD COLUMN IF NOT EXISTS claimed_by text;
ALTER TABLE runs ADD COLUMN IF NOT EXISTS lease_expires timestamptz;
CREATE INDEX IF NOT EXISTS runs_created_idx ON runs (created_at DESC);
CREATE INDEX IF NOT EXISTS runs_claim_idx ON runs (status, lease_expires);
CREATE INDEX IF NOT EXISTS runs_project_idx ON runs (project_id);
CREATE INDEX IF NOT EXISTS workflows_project_idx ON workflows (project_id);
CREATE TABLE IF NOT EXISTS secrets (
    project_id text NOT NULL DEFAULT 'default',
    name       text NOT NULL,
    ciphertext bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, name)
);
CREATE TABLE IF NOT EXISTS project_members (
    project_id text NOT NULL,
    user_id    text NOT NULL,
    role       text NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (project_id, user_id)
);
CREATE TABLE IF NOT EXISTS audit (
    id       text PRIMARY KEY,
    ts       timestamptz NOT NULL,
    actor    text NOT NULL,
    method   text NOT NULL,
    path     text NOT NULL,
    project  text NOT NULL,
    status   int NOT NULL
);
CREATE INDEX IF NOT EXISTS audit_ts_idx ON audit (ts DESC);
CREATE TABLE IF NOT EXISTS users (
    id            text PRIMARY KEY,
    email         text UNIQUE NOT NULL,
    name          text NOT NULL,
    role          text NOT NULL,
    password_hash text NOT NULL,
    created_at    timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS api_tokens (
    id           text PRIMARY KEY,
    name         text NOT NULL,
    hash         text UNIQUE NOT NULL,
    role         text NOT NULL,
    created_at   timestamptz NOT NULL,
    last_used_at timestamptz
);
`

// NewPostgres opens a connection pool, verifies it, and applies the schema.
func NewPostgres(dsn string) (*Pg, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetConnMaxIdleTime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping: %w", err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Pg{db: db, dsn: dsn, broker: newBroker()}, nil
}

// Close releases the connection pool.
func (s *Pg) Close() error { return s.db.Close() }

func (s *Pg) Broker() *Broker { return s.broker }

// StartEventRelay wires the broker to Postgres LISTEN/NOTIFY so run events fan
// out across every server instance. Published events are forwarded via NOTIFY,
// and a background LISTEN loop delivers remote events into the local broker.
func (s *Pg) StartEventRelay(ctx context.Context) {
	s.broker.SetRelay(s.notify)
	go s.listenLoop(ctx)
}

// notify forwards an event to other instances via NOTIFY. Heavy snapshot fields
// are stripped to stay within Postgres's ~8KB payload limit; subscribers that
// need the full run refetch it over the API.
func (s *Pg) notify(e model.Event) {
	e.Step = nil
	e.Run = nil
	b, err := json.Marshal(e)
	if err != nil || len(b) > 7500 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_, _ = s.db.ExecContext(ctx, "SELECT pg_notify($1,$2)", pgNotifyChannel, string(b))
	}()
}

// listenLoop LISTENs for events from other instances and delivers them locally.
// It reconnects with backoff until the context is canceled.
func (s *Pg) listenLoop(ctx context.Context) {
	for ctx.Err() == nil {
		if err := s.listenOnce(ctx); err != nil && ctx.Err() == nil {
			log.Printf("event relay: %v; reconnecting in 2s", err)
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
			}
		}
	}
}

func (s *Pg) listenOnce(ctx context.Context) error {
	conn, err := pgx.Connect(ctx, s.dsn)
	if err != nil {
		return err
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(ctx, "LISTEN "+pgNotifyChannel); err != nil {
		return err
	}
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var e model.Event
		if json.Unmarshal([]byte(n.Payload), &e) == nil {
			s.broker.Deliver(e) // local-only; no re-notify
		}
	}
}

func (s *Pg) PutWorkflow(w model.Workflow) error {
	b, err := json.Marshal(w)
	if err != nil {
		return err
	}
	pid := w.ProjectID
	if pid == "" {
		pid = model.DefaultProjectID
	}
	_, err = s.db.Exec(
		`INSERT INTO workflows (id, project_id, name, data, updated_at) VALUES ($1,$2,$3,$4,now())
		 ON CONFLICT (id) DO UPDATE SET project_id=EXCLUDED.project_id, name=EXCLUDED.name, data=EXCLUDED.data, updated_at=now()`,
		w.ID, pid, w.Name, b)
	return err
}

func (s *Pg) CreateProject(p model.Project) error {
	_, err := s.db.Exec(
		`INSERT INTO projects (id, name, created_at) VALUES ($1,$2,$3)
		 ON CONFLICT (id) DO NOTHING`,
		p.ID, p.Name, p.CreatedAt)
	return err
}

func (s *Pg) GetProject(id string) (model.Project, bool, error) {
	var p model.Project
	err := s.db.QueryRow(`SELECT id,name,created_at FROM projects WHERE id=$1`, id).Scan(&p.ID, &p.Name, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return model.Project{}, false, nil
	}
	return p, err == nil, err
}

func (s *Pg) ListProjects() ([]model.Project, error) {
	rows, err := s.db.Query(`SELECT id,name,created_at FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Project
	for rows.Next() {
		var p model.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Pg) GetWorkflow(id string) (model.Workflow, bool, error) {
	var b []byte
	err := s.db.QueryRow(`SELECT data FROM workflows WHERE id=$1`, id).Scan(&b)
	if err == sql.ErrNoRows {
		return model.Workflow{}, false, nil
	}
	if err != nil {
		return model.Workflow{}, false, err
	}
	var w model.Workflow
	if err := json.Unmarshal(b, &w); err != nil {
		return model.Workflow{}, false, err
	}
	return w, true, nil
}

func (s *Pg) ListWorkflows(projectID string) ([]model.Workflow, error) {
	q := `SELECT data FROM workflows`
	args := []any{}
	if projectID != "" {
		q += ` WHERE project_id=$1`
		args = append(args, projectID)
	}
	q += ` ORDER BY name`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Workflow
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		var w model.Workflow
		if err := json.Unmarshal(b, &w); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Pg) CreateRun(r *model.Run) (model.Run, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return model.Run{}, err
	}
	pid := r.ProjectID
	if pid == "" {
		pid = model.DefaultProjectID
	}
	_, err = s.db.Exec(
		`INSERT INTO runs (id, workflow_id, project_id, status, data, created_at, finished_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		r.ID, r.WorkflowID, pid, string(r.Status), b, r.CreatedAt, r.FinishedAt)
	if err != nil {
		return model.Run{}, err
	}
	return cloneRun(r), nil
}

func (s *Pg) GetRun(id string) (model.Run, bool, error) {
	var b []byte
	err := s.db.QueryRow(`SELECT data FROM runs WHERE id=$1`, id).Scan(&b)
	if err == sql.ErrNoRows {
		return model.Run{}, false, nil
	}
	if err != nil {
		return model.Run{}, false, err
	}
	var r model.Run
	if err := json.Unmarshal(b, &r); err != nil {
		return model.Run{}, false, err
	}
	return r, true, nil
}

func (s *Pg) ListRuns(projectID string) ([]model.Run, error) {
	q := `SELECT data FROM runs`
	args := []any{}
	if projectID != "" {
		q += ` WHERE project_id=$1`
		args = append(args, projectID)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Run
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		var r model.Run
		if err := json.Unmarshal(b, &r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// MutateRun loads the run FOR UPDATE, applies fn, and writes it back in one
// transaction so concurrent step updates serialize on the row.
func (s *Pg) MutateRun(id string, fn func(*model.Run)) (model.Run, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Run{}, false, err
	}
	defer tx.Rollback() //nolint:errcheck // rolled back unless we commit

	var b []byte
	err = tx.QueryRowContext(ctx, `SELECT data FROM runs WHERE id=$1 FOR UPDATE`, id).Scan(&b)
	if err == sql.ErrNoRows {
		return model.Run{}, false, nil
	}
	if err != nil {
		return model.Run{}, false, err
	}
	var r model.Run
	if err := json.Unmarshal(b, &r); err != nil {
		return model.Run{}, false, err
	}

	fn(&r)

	nb, err := json.Marshal(&r)
	if err != nil {
		return model.Run{}, false, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE runs SET status=$2, data=$3, finished_at=$4 WHERE id=$1`,
		id, string(r.Status), nb, r.FinishedAt); err != nil {
		return model.Run{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return model.Run{}, false, err
	}
	return r, true, nil
}

func (s *Pg) PutSecret(projectID, name string, ciphertext []byte) error {
	_, err := s.db.Exec(
		`INSERT INTO secrets (project_id, name, ciphertext, created_at) VALUES ($1,$2,$3,now())
		 ON CONFLICT (project_id, name) DO UPDATE SET ciphertext=EXCLUDED.ciphertext, created_at=now()`,
		projectID, name, ciphertext)
	return err
}

func (s *Pg) GetSecret(projectID, name string) ([]byte, bool, error) {
	var b []byte
	err := s.db.QueryRow(`SELECT ciphertext FROM secrets WHERE project_id=$1 AND name=$2`, projectID, name).Scan(&b)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

func (s *Pg) ListSecretNames(projectID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT name FROM secrets WHERE project_id=$1 ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Pg) DeleteSecret(projectID, name string) error {
	_, err := s.db.Exec(`DELETE FROM secrets WHERE project_id=$1 AND name=$2`, projectID, name)
	return err
}

func (s *Pg) AddProjectMember(m model.ProjectMember) error {
	_, err := s.db.Exec(
		`INSERT INTO project_members (project_id, user_id, role, created_at) VALUES ($1,$2,$3,$4)
		 ON CONFLICT (project_id, user_id) DO UPDATE SET role=EXCLUDED.role`,
		m.ProjectID, m.UserID, string(m.Role), m.CreatedAt)
	return err
}

func (s *Pg) GetProjectMember(projectID, userID string) (model.ProjectMember, bool, error) {
	var m model.ProjectMember
	var role string
	err := s.db.QueryRow(`SELECT project_id,user_id,role,created_at FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, userID).
		Scan(&m.ProjectID, &m.UserID, &role, &m.CreatedAt)
	if err == sql.ErrNoRows {
		return model.ProjectMember{}, false, nil
	}
	m.Role = model.Role(role)
	return m, err == nil, err
}

func (s *Pg) ListProjectMembers(projectID string) ([]model.ProjectMember, error) {
	rows, err := s.db.Query(`SELECT project_id,user_id,role,created_at FROM project_members WHERE project_id=$1 ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ProjectMember{}
	for rows.Next() {
		var m model.ProjectMember
		var role string
		if err := rows.Scan(&m.ProjectID, &m.UserID, &role, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.Role = model.Role(role)
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Pg) RemoveProjectMember(projectID, userID string) error {
	_, err := s.db.Exec(`DELETE FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, userID)
	return err
}

// ClaimRun atomically leases one claimable run using SELECT ... FOR UPDATE SKIP
// LOCKED, so concurrent workers across instances never grab the same run.
func (s *Pg) ClaimRun(workerID string, lease time.Duration) (model.Run, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Run{}, false, err
	}
	defer tx.Rollback() //nolint:errcheck

	var id string
	var data []byte
	err = tx.QueryRowContext(ctx, `
		SELECT id, data FROM runs
		WHERE status = 'queued'
		   OR (status IN ('running','waiting') AND lease_expires IS NOT NULL AND lease_expires < now())
		ORDER BY created_at
		FOR UPDATE SKIP LOCKED
		LIMIT 1`).Scan(&id, &data)
	if err == sql.ErrNoRows {
		return model.Run{}, false, nil
	}
	if err != nil {
		return model.Run{}, false, err
	}
	var r model.Run
	if err := json.Unmarshal(data, &r); err != nil {
		return model.Run{}, false, err
	}
	r.Status = model.StatusRunning
	nb, _ := json.Marshal(&r)
	if _, err := tx.ExecContext(ctx,
		`UPDATE runs SET status='running', data=$2, claimed_by=$3, lease_expires=now()+$4::interval WHERE id=$1`,
		id, nb, workerID, fmt.Sprintf("%d milliseconds", lease.Milliseconds())); err != nil {
		return model.Run{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return model.Run{}, false, err
	}
	return r, true, nil
}

func (s *Pg) RenewLease(runID, workerID string, lease time.Duration) error {
	_, err := s.db.Exec(
		`UPDATE runs SET lease_expires=now()+$3::interval WHERE id=$1 AND claimed_by=$2`,
		runID, workerID, fmt.Sprintf("%d milliseconds", lease.Milliseconds()))
	return err
}

func (s *Pg) AppendAudit(e model.AuditEntry) error {
	_, err := s.db.Exec(
		`INSERT INTO audit (id, ts, actor, method, path, project, status) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		e.ID, e.Time, e.Actor, e.Method, e.Path, e.Project, e.Status)
	return err
}

func (s *Pg) ListAudit(limit int) ([]model.AuditEntry, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.Query(`SELECT id,ts,actor,method,path,project,status FROM audit ORDER BY ts DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.AuditEntry{}
	for rows.Next() {
		var e model.AuditEntry
		if err := rows.Scan(&e.ID, &e.Time, &e.Actor, &e.Method, &e.Path, &e.Project, &e.Status); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Pg) CreateUser(u model.User) error {
	_, err := s.db.Exec(
		`INSERT INTO users (id, email, name, role, password_hash, created_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		u.ID, u.Email, u.Name, string(u.Role), u.PasswordHash, u.CreatedAt)
	return err
}

func scanUser(row interface{ Scan(...any) error }) (model.User, error) {
	var u model.User
	var role string
	err := row.Scan(&u.ID, &u.Email, &u.Name, &role, &u.PasswordHash, &u.CreatedAt)
	u.Role = model.Role(role)
	return u, err
}

func (s *Pg) GetUserByEmail(email string) (model.User, bool, error) {
	u, err := scanUser(s.db.QueryRow(`SELECT id,email,name,role,password_hash,created_at FROM users WHERE email=$1`, email))
	if err == sql.ErrNoRows {
		return model.User{}, false, nil
	}
	return u, err == nil, err
}

func (s *Pg) GetUserByID(id string) (model.User, bool, error) {
	u, err := scanUser(s.db.QueryRow(`SELECT id,email,name,role,password_hash,created_at FROM users WHERE id=$1`, id))
	if err == sql.ErrNoRows {
		return model.User{}, false, nil
	}
	return u, err == nil, err
}

func (s *Pg) ListUsers() ([]model.User, error) {
	rows, err := s.db.Query(`SELECT id,email,name,role,password_hash,created_at FROM users ORDER BY email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Pg) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Pg) CreateAPIToken(t model.APIToken) error {
	_, err := s.db.Exec(
		`INSERT INTO api_tokens (id, name, hash, role, created_at, last_used_at) VALUES ($1,$2,$3,$4,$5,$6)`,
		t.ID, t.Name, t.Hash, string(t.Role), t.CreatedAt, t.LastUsedAt)
	return err
}

func scanToken(row interface{ Scan(...any) error }) (model.APIToken, error) {
	var t model.APIToken
	var role string
	err := row.Scan(&t.ID, &t.Name, &t.Hash, &role, &t.CreatedAt, &t.LastUsedAt)
	t.Role = model.Role(role)
	return t, err
}

func (s *Pg) GetAPITokenByHash(hash string) (model.APIToken, bool, error) {
	t, err := scanToken(s.db.QueryRow(`SELECT id,name,hash,role,created_at,last_used_at FROM api_tokens WHERE hash=$1`, hash))
	if err == sql.ErrNoRows {
		return model.APIToken{}, false, nil
	}
	return t, err == nil, err
}

func (s *Pg) ListAPITokens() ([]model.APIToken, error) {
	rows, err := s.db.Query(`SELECT id,name,hash,role,created_at,last_used_at FROM api_tokens ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.APIToken
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Pg) DeleteAPIToken(id string) error {
	_, err := s.db.Exec(`DELETE FROM api_tokens WHERE id=$1`, id)
	return err
}

var _ Store = (*Pg)(nil)
