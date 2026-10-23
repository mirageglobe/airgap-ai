package ari

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// RowSink stores the explorer's rows where Zetaris can read them.
type RowSink interface {
	Put(run string, r Row) error
}

// PostgresSink writes rows to the evidence database registered in Zetaris.
type PostgresSink struct {
	dsn  string
	conn *pgx.Conn
}

func NewPostgresSink(dsn string) (*PostgresSink, error) {
	conn, err := connect(dsn)
	if err != nil {
		return nil, err
	}
	return &PostgresSink{dsn: dsn, conn: conn}, nil
}

func connect(dsn string) (*pgx.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := conn.Ping(ctx); err != nil {
		conn.Close(ctx)
		return nil, err
	}
	return conn, nil
}

// Put writes one row. A failed write reconnects once and retries, so a
// restarted evidence db does not leave ari blind until it restarts too.
func (s *PostgresSink) Put(run string, r Row) error {
	data, err := json.Marshal(r.Data)
	if err != nil {
		return err
	}
	if err = s.insert(run, r, data); err == nil {
		return nil
	}
	conn, cerr := connect(s.dsn)
	if cerr != nil {
		return fmt.Errorf("%w; reconnect: %v", err, cerr)
	}
	s.conn.Close(context.Background())
	s.conn = conn
	return s.insert(run, r, data)
}

func (s *PostgresSink) insert(run string, r Row, data []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := s.conn.Exec(ctx,
		"INSERT INTO evidence_rows (run, id, cycle, kind, data) VALUES ($1, $2, $3, $4, $5)",
		run, r.ID, r.Cycle, r.Kind, string(data))
	return err
}

func (s *PostgresSink) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.conn.Close(ctx)
}
