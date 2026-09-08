package db

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

type migrationExecResult struct {
	err error
}

type migrationExecutor struct {
	results []migrationExecResult
	queries []string
}

func (e *migrationExecutor) Exec(query string, _ ...any) (sql.Result, error) {
	e.queries = append(e.queries, strings.Join(strings.Fields(query), " "))
	result := e.results[len(e.queries)-1]
	return nil, result.err
}

func TestEnsureCallReviewColumns(t *testing.T) {
	t.Run("applies column backfill and index", func(t *testing.T) {
		exec := &migrationExecutor{results: make([]migrationExecResult, 3)}

		require.NoError(t, ensureCallReviewColumns(exec))
		require.Len(t, exec.queries, 3)
		require.Contains(t, exec.queries[0], "ADD COLUMN lead_id")
		require.Contains(t, exec.queries[1], "SET cr.lead_id = ct.lead_id")
		require.Contains(t, exec.queries[2], "ADD INDEX idx_call_reviews_lead_id")
	})

	t.Run("accepts an already migrated schema", func(t *testing.T) {
		exec := &migrationExecutor{results: []migrationExecResult{
			{err: &mysql.MySQLError{Number: 1060, Message: "Duplicate column name 'lead_id'"}},
			{},
			{err: &mysql.MySQLError{Number: 1061, Message: "Duplicate key name 'idx_call_reviews_lead_id'"}},
		}}

		require.NoError(t, ensureCallReviewColumns(exec))
		require.Len(t, exec.queries, 3)
	})

	t.Run("stops when backfill fails", func(t *testing.T) {
		exec := &migrationExecutor{results: []migrationExecResult{
			{},
			{err: errors.New("database unavailable")},
		}}

		err := ensureCallReviewColumns(exec)
		require.ErrorContains(t, err, "backfill call_reviews.lead_id")
		require.Len(t, exec.queries, 2)
	})
}
