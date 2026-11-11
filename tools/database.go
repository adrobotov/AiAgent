package tools

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DatabaseTool provides database query operations.
// Supports SQLite (via modernc.org/sqlite) and PostgreSQL (via subprocess).
type DatabaseTool struct {
	// maxRows limits the number of rows returned per query.
	maxRows int
	// timeout is the maximum execution time for queries.
	timeout time.Duration
}

// NewDatabaseTool creates a new database tool.
func NewDatabaseTool(maxRows int, timeout time.Duration) *DatabaseTool {
	if maxRows <= 0 {
		maxRows = 100
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &DatabaseTool{
		maxRows: maxRows,
		timeout: timeout,
	}
}

// Name returns the tool name.
func (t *DatabaseTool) Name() string { return "database" }

// Description returns the tool description.
func (DatabaseTool) Description() string {
	return "Executes SQL queries against SQLite databases. Supports SELECT, schema inspection, and CRUD operations."
}

// Parameters returns the tool parameters schema.
func (DatabaseTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"query", "schema", "tables", "describe"},
				"description": "The database operation to perform.",
			},
			"db": map[string]interface{}{
				"type":        "string",
				"description": "Path to the SQLite database file.",
			},
			"sql": map[string]interface{}{
				"type":        "string",
				"description": "SQL query to execute.",
			},
			"table": map[string]interface{}{
				"type":        "string",
				"description": "Table name (for 'describe' and 'tables' actions).",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of rows to return (default: 100).",
			},
		},
		"required": []string{"action"},
	}
}

// Execute executes the database operation.
func (t *DatabaseTool) Execute(argsJSON string) (string, error) {
	var args struct {
		Action string `json:"action"`
		DB     string `json:"db"`
		SQL    string `json:"sql"`
		Table  string `json:"table"`
		Limit  int    `json:"limit"`
	}
	if err := parseArgs(argsJSON, &args); err != nil {
		return "", err
	}

	action := strings.ToLower(strings.TrimSpace(args.Action))
	if action == "" {
		return "", fmt.Errorf("action is required")
	}

	if args.Limit <= 0 {
		args.Limit = t.maxRows
	}

	switch action {
	case "query":
		if args.SQL == "" {
			return "", fmt.Errorf("sql query is required")
		}
		return t.executeQuery(args.SQL, args.Limit)
	case "schema":
		if args.SQL == "" {
			return "", fmt.Errorf("sql query is required (use 'PRAGMA table_info(table_name)' to inspect schema)")
		}
		return t.executeQuery(args.SQL, args.Limit)
	case "tables":
		return t.listTables(args.DB, args.Table)
	case "describe":
		if args.Table == "" {
			return "", fmt.Errorf("table name is required for describe action")
		}
		return t.describeTable(args.DB, args.Table)
	default:
		return "", fmt.Errorf("unknown action: %s. Available: query, schema, tables, describe", action)
	}
}

// executeQuery runs a SQL query and returns results as JSON.
func (t *DatabaseTool) executeQuery(sql string, limit int) (string, error) {
	db, err := t.openDB("")
	if err != nil {
		return "", fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	// Set query timeout
	_, _ = db.Exec("PRAGMA busy_timeout = 5000")

	// Add LIMIT if not present and it's a SELECT
	lowerSQL := strings.ToLower(strings.TrimSpace(sql))
	if strings.HasPrefix(lowerSQL, "select") && !strings.Contains(lowerSQL, "limit") {
		sql = sql + fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := db.Query(sql)
	if err != nil {
		return "", fmt.Errorf("query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return "", fmt.Errorf("get columns: %w", err)
	}

	if len(columns) == 0 {
		// Non-SELECT query (INSERT, UPDATE, DELETE)
		return "Query executed successfully (no rows returned)", nil
	}

	var results []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return "", fmt.Errorf("scan row: %w", err)
		}

		row := make(map[string]interface{})
		for i, col := range columns {
			val := values[i]
			if val == nil {
				row[col] = nil
			} else if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("rows iteration: %w", err)
	}

	output, err := json.MarshalIndent(map[string]interface{}{
		"columns": columns,
		"rows":    results,
		"count":   len(results),
	}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal results: %w", err)
	}

	return string(output), nil
}

// listTables lists tables in the database.
func (t *DatabaseTool) listTables(dbPath, tableName string) (string, error) {
	db, err := t.openDB(dbPath)
	if err != nil {
		return "", fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	var sql string
	if tableName != "" {
		sql = fmt.Sprintf("SELECT sql FROM sqlite_master WHERE type='table' AND name='%s'", tableName)
	} else {
		sql = "SELECT name, sql FROM sqlite_master WHERE type='table' ORDER BY name"
	}

	return t.executeQuery(sql, 100)
}

// describeTable returns the schema for a specific table.
func (t *DatabaseTool) describeTable(dbPath, tableName string) (string, error) {
	db, err := t.openDB(dbPath)
	if err != nil {
		return "", fmt.Errorf("open database: %w", err)
	}
	defer func() { _ = db.Close() }()

	sql := fmt.Sprintf("PRAGMA table_info('%s')", tableName)
	return t.executeQuery(sql, 100)
}

// openDB opens a SQLite database connection.
// If dbPath is empty, opens an in-memory database.
func (t *DatabaseTool) openDB(dbPath string) (*sql.DB, error) {
	if dbPath == "" {
		dbPath = ":memory:"
	}
	return sql.Open("sqlite", dbPath)
}
