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
	d := &DB{pool: pool}
	if err := d.EnsureOrganizationsTable(); err != nil {
		return nil, fmt.Errorf("db.New: ensure organizations table: %w", err)
	}
	if err := d.EnsureAdminSubscriptionsTable(); err != nil {
		return nil, fmt.Errorf("db.New: ensure admin subscriptions table: %w", err)
	}
	if err := d.EnsureUserFeatureFlagsTable(); err != nil {
		return nil, fmt.Errorf("db.New: ensure user feature flags table: %w", err)
	}
	if err := d.EnsureOrgExotelAccountsTable(); err != nil {
		return nil, fmt.Errorf("db.New: ensure org exotel accounts table: %w", err)
	}
	if err := d.EnsureUserAllowedExotelAccountsTable(); err != nil {
		return nil, fmt.Errorf("db.New: ensure user allowed exotel accounts table: %w", err)
	}
	if err := d.EnsureProductsTable(); err != nil {
		return nil, fmt.Errorf("db.New: ensure products table: %w", err)
	}
	if err := d.EnsureCampaignsTable(); err != nil {
		return nil, fmt.Errorf("db.New: ensure campaigns table: %w", err)
	}
	if err := d.EnsureExecutivesTable(); err != nil {
		return nil, fmt.Errorf("db.New: ensure executives table: %w", err)
	}
	if err := d.EnsureScheduledCallsTable(); err != nil {
		return nil, fmt.Errorf("db.New: ensure scheduled calls table: %w", err)
	}
	if err := d.EnsureRBACTables(); err != nil {
		return nil, fmt.Errorf("db.New: ensure RBAC tables: %w", err)
	}
	if err := d.EnsureAgentPresenceTable(); err != nil {
		return nil, fmt.Errorf("db.New: ensure agent presence table: %w", err)
	}
	if err := d.EnsureAgentActivitiesTable(); err != nil {
		return nil, fmt.Errorf("db.New: ensure agent activities table: %w", err)
	}
	if err := d.EnsureCallTranscriptColumns(); err != nil {
		return nil, fmt.Errorf("db.New: ensure call transcript columns: %w", err)
	}
	if err := d.EnsureCallReviewColumns(); err != nil {
		return nil, fmt.Errorf("db.New: ensure call review columns: %w", err)
	}
	if err := d.EnsureAPIKeysTable(); err != nil {
		return nil, fmt.Errorf("db.New: ensure API keys table: %w", err)
	}
	return d, nil
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
