// Package db provides a MySQL data layer for the Callified REST API.
// All functions mirror the query semantics of database.py.
package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// DB wraps *sql.DB and provides all query methods.
type DB struct {
	pool *sql.DB
}

// New opens a MySQL connection pool from the given DSN and verifies connectivity.
// DSN format: "user:password@tcp(host:3306)/dbname?parseTime=true"
func New(dsn string) (*DB, error) {
	pool, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("db.New: open: %w", err)
	}
	pool.SetMaxOpenConns(25)
	pool.SetMaxIdleConns(10)
	pool.SetConnMaxLifetime(5 * time.Minute)
	if err := pool.Ping(); err != nil {
		return nil, fmt.Errorf("db.New: ping: %w", err)
	}
	return &DB{pool: pool}, nil
}

// Close closes the underlying connection pool.
func (d *DB) Close() error { return d.pool.Close() }

// Ping verifies the database connection is still alive.
func (d *DB) Ping() error { return d.pool.Ping() }

// nullString converts an empty string to sql.NullString.
func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// nullInt64 converts 0 to sql.NullInt64.
func nullInt64(i int64) sql.NullInt64 {
	return sql.NullInt64{Int64: i, Valid: i != 0}
}
