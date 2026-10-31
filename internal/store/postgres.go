package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"conduit/internal/model"

	_ "github.com/jackc/pgx/v5/stdlib" // register the "pgx" database/sql driver
)

// Pg is a Postgres-backed Store. Workflows and runs are stored as JSONB with a
// few promoted columns for ordering and filtering. The live-event broker stays
// in-process (events are ephemeral), so a single server instance owns the stream.
type Pg struct {
	db     *sql.DB
	broker *Broker
}

const schema = `
CREATE TABLE IF NOT EXISTS workflows (
    id         text PRIMARY KEY,
    name       text NOT NULL,
    data       jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS runs (
    id          text PRIMARY KEY,
    workflow_id text NOT NULL,
    status      text NOT NULL,
    data        jsonb NOT NULL,
    created_at  timestamptz NOT NULL,
    finished_at timestamptz
);
CREATE INDEX IF NOT EXISTS runs_created_idx ON runs (created_at DESC);
CREATE TABLE IF NOT EXISTS secrets (
    name       text PRIMARY KEY,
    ciphertext bytea NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
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
	return &Pg{db: db, broker: newBroker()}, nil
}

// Close releases the connection pool.
func (s *Pg) Close() error { return s.db.Close() }

func (s *Pg) Broker() *Broker { return s.broker }

func (s *Pg) PutWorkflow(w model.Workflow) error {
	b, err := json.Marshal(w)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO workflows (id, name, data, updated_at) VALUES ($1,$2,$3,now())
		 ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, data=EXCLUDED.data, updated_at=now()`,
		w.ID, w.Name, b)
	return err
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

func (s *Pg) ListWorkflows() ([]model.Workflow, error) {
	rows, err := s.db.Query(`SELECT data FROM workflows ORDER BY name`)
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
	_, err = s.db.Exec(
		`INSERT INTO runs (id, workflow_id, status, data, created_at, finished_at)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		r.ID, r.WorkflowID, string(r.Status), b, r.CreatedAt, r.FinishedAt)
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

func (s *Pg) ListRuns() ([]model.Run, error) {
	rows, err := s.db.Query(`SELECT data FROM runs ORDER BY created_at DESC`)
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

func (s *Pg) PutSecret(name string, ciphertext []byte) error {
	_, err := s.db.Exec(
		`INSERT INTO secrets (name, ciphertext, created_at) VALUES ($1,$2,now())
		 ON CONFLICT (name) DO UPDATE SET ciphertext=EXCLUDED.ciphertext, created_at=now()`,
		name, ciphertext)
	return err
}

func (s *Pg) GetSecret(name string) ([]byte, bool, error) {
	var b []byte
	err := s.db.QueryRow(`SELECT ciphertext FROM secrets WHERE name=$1`, name).Scan(&b)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}

func (s *Pg) ListSecretNames() ([]string, error) {
	rows, err := s.db.Query(`SELECT name FROM secrets ORDER BY name`)
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

func (s *Pg) DeleteSecret(name string) error {
	_, err := s.db.Exec(`DELETE FROM secrets WHERE name=$1`, name)
	return err
}

var _ Store = (*Pg)(nil)
