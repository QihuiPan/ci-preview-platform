// Package persistence commits control-plane operations before acknowledging them.
package persistence

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/QihuiPan/ci-preview-platform/internal/control"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Backend runs a short state transaction. Network effects must stay outside callbacks.
type Backend interface {
	Transact(context.Context, bool, string, func(*control.Store, time.Time) error) error
	Ping(context.Context) error
	Close()
}

// Memory is only for unit tests and explicitly requested nonpersistent experiments.
type Memory struct {
	mu    sync.Mutex
	Store *control.Store
}

func (m *Memory) Transact(ctx context.Context, write bool, action string, f func(*control.Store, time.Time) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !write {
		return f(m.Store, time.Now().UTC())
	}
	copy, e := m.Store.Copy()
	if e != nil {
		return e
	}
	if e = f(copy, time.Now().UTC()); e != nil {
		return e
	}
	m.Store = copy
	return nil
}
func (m *Memory) Ping(context.Context) error { return nil }
func (m *Memory) Close()                     {}

// Postgres serializes small-team scheduler decisions across API replicas.
// State is encrypted at rest because assignments include short-lived lease secrets.
type Postgres struct {
	pool   *pgxpool.Pool
	config control.Config
	aead   cipher.AEAD
}

func Open(ctx context.Context, dsn string, key []byte, config control.Config) (*Postgres, error) {
	if len(key) != 32 {
		return nil, errors.New("STATE_KEY must decode to exactly 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	pc, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("invalid DATABASE_URL")
	}
	pc.MaxConns = 8
	pc.MinConns = 1
	pc.ConnConfig.ConnectTimeout = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, errors.New("cannot create database pool")
	}
	p := &Postgres{pool, config, aead}
	if err = p.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("initialize database: %w", err)
	}
	if err = p.Transact(ctx, false, "", func(*control.Store, time.Time) error { return nil }); err != nil {
		pool.Close()
		return nil, err
	}
	return p, nil
}

func (p *Postgres) migrate(ctx context.Context) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(72198123)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS control_state (
 id integer PRIMARY KEY CHECK(id=1), schema_version integer NOT NULL CHECK(schema_version=1), payload bytea NOT NULL, updated_at timestamptz NOT NULL DEFAULT now());
 CREATE TABLE IF NOT EXISTS control_audit (id bigserial PRIMARY KEY, occurred_at timestamptz NOT NULL DEFAULT now(), action text NOT NULL);
 CREATE INDEX IF NOT EXISTS control_audit_occurred_at ON control_audit(occurred_at);`); err != nil {
		return err
	}
	raw, err := control.New(p.config).Snapshot()
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO control_state(id,schema_version,payload) VALUES (1,1,$1) ON CONFLICT DO NOTHING", p.seal(raw)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (p *Postgres) Transact(ctx context.Context, write bool, action string, f func(*control.Store, time.Time) error) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := p.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	query := "SELECT payload FROM control_state WHERE id=1"
	if write {
		query += " FOR UPDATE"
	}
	var encrypted []byte
	if err = tx.QueryRow(ctx, query).Scan(&encrypted); err != nil {
		return err
	}
	raw, err := p.open(encrypted)
	if err != nil {
		return errors.New("cannot decrypt persisted state; verify STATE_KEY")
	}
	s, err := control.Restore(p.config, raw)
	if err != nil {
		return err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return err
	}
	if err = f(s, now); err != nil {
		return err
	}
	if write {
		payload, e := s.Snapshot()
		if e != nil {
			return e
		}
		if _, err = tx.Exec(ctx, "UPDATE control_state SET payload=$1,updated_at=clock_timestamp() WHERE id=1", p.seal(payload)); err != nil {
			return err
		}
		if action != "" {
			if _, err = tx.Exec(ctx, "INSERT INTO control_audit(action) VALUES($1)", action); err != nil {
				return err
			}
		}
	}
	return tx.Commit(ctx)
}
func (p *Postgres) seal(raw []byte) []byte {
	nonce := make([]byte, p.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	return p.aead.Seal(nonce, nonce, raw, []byte("ci-preview-state-v1"))
}
func (p *Postgres) open(raw []byte) ([]byte, error) {
	n := p.aead.NonceSize()
	if len(raw) < n {
		return nil, errors.New("invalid state envelope")
	}
	return p.aead.Open(nil, raw[:n], raw[n:], []byte("ci-preview-state-v1"))
}
func (p *Postgres) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }
func (p *Postgres) Close()                         { p.pool.Close() }
