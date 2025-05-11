package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/denisenkom/go-mssqldb"
	_ "github.com/lib/pq"
)

func main() {
	// Define command line flags
	sourceDsnFlag := flag.String("source-dsn", "", "SQL Server connection string (e.g., sqlserver://user:pass@host:1433?database=yourdb)")
	targetDsnFlag := flag.String("target-dsn", "", "PostgreSQL connection string (e.g., postgres://user:pass@host:5432/dbname?sslmode=disable)")
	batchSizeFlag := flag.Int("batch-size", 1000, "Number of rows to process in each batch")
	tablesFlag := flag.String("tables", "", "Comma-separated list of tables to migrate (default: all)")
	excludeTablesFlag := flag.String("exclude-tables", "", "Comma-separated list of tables to exclude from migration")
	schemasFlag := flag.String("schemas", "dbo", "Comma-separated list of schemas to include (default: dbo)")
	truncateFlag := flag.Bool("truncate", false, "Whether to truncate target tables before migration")
	flag.Parse()

	// Determine the source DSN to use (command line arg -> environment variable -> default)
	sourceDsn := *sourceDsnFlag
	if sourceDsn == "" {
		// If not provided via command line, check environment variable
		sourceDsn = os.Getenv("SOURCE_DB_DSN")
		if sourceDsn == "" {
			// If still not provided, use default
			sourceDsn = "sqlserver://user:pass@host:1433?database=yourdb"
			fmt.Println("Warning: Using default source database connection. Consider setting SOURCE_DB_DSN environment variable or using -source-dsn flag.")
		}
	}

	// Check if the protocol is correct (should be sqlserver:// not mssql://)
	if strings.HasPrefix(sourceDsn, "mssql://") {
		sourceDsn = "sqlserver://" + sourceDsn[8:]
		fmt.Println("Warning: Changed protocol from mssql:// to sqlserver://")
	}

	// Ensure the connection string has the required parameters for SQL Server
	// This is especially important for AWS RDS instances

	// Start with the base DSN
	dsnParams := make(map[string]string)

	// Parse existing parameters
	if strings.Contains(sourceDsn, "?") {
		parts := strings.SplitN(sourceDsn, "?", 2)
		baseURL := parts[0]
		params := parts[1]

		// Extract existing parameters
		for _, param := range strings.Split(params, "&") {
			if param == "" {
				continue
			}
			keyValue := strings.SplitN(param, "=", 2)
			if len(keyValue) == 2 {
				dsnParams[keyValue[0]] = keyValue[1]
			}
		}

		// Remove parameters from the base DSN
		sourceDsn = baseURL
	}

	// Add required parameters if not already present
	if _, ok := dsnParams["connection timeout"]; !ok {
		dsnParams["connection timeout"] = "30"
	}

	// Add parameters for all connections
	if _, ok := dsnParams["encrypt"]; !ok {
		dsnParams["encrypt"] = "disable"
	}
	if _, ok := dsnParams["browser"]; !ok {
		dsnParams["browser"] = "disable"
	}
	if _, ok := dsnParams["dial timeout"]; !ok {
		dsnParams["dial timeout"] = "10"
	}

	// For AWS RDS instances, add specific parameters
	if strings.Contains(sourceDsn, "rds.amazonaws.com") {
		if _, ok := dsnParams["server sni"]; !ok {
			dsnParams["server sni"] = "disable"
		}
		// Ensure we're using the host from the connection string, not localhost
		if _, ok := dsnParams["server"]; !ok {
			// Extract host from connection string
			host := ""
			if strings.Contains(sourceDsn, "@") {
				parts := strings.Split(sourceDsn, "@")
				if len(parts) > 1 {
					hostPort := strings.Split(parts[1], "/")
					host = hostPort[0]
					if strings.Contains(host, ":") {
						host = strings.Split(host, ":")[0]
					}
					dsnParams["server"] = host
				}
			}
		}
	}

	// Rebuild the connection string
	if len(dsnParams) > 0 {
		sourceDsn += "?"
		paramStrings := make([]string, 0, len(dsnParams))
		for key, value := range dsnParams {
			paramStrings = append(paramStrings, key+"="+value)
		}
		sourceDsn += strings.Join(paramStrings, "&")
	}

	fmt.Printf("Connecting to SQL Server source with DSN: %s\n", sourceDsn)

	// Determine the target DSN to use (command line arg -> environment variable -> default)
	targetDsn := *targetDsnFlag
	if targetDsn == "" {
		// If not provided via command line, check environment variable
		targetDsn = os.Getenv("TARGET_DB_DSN")
		if targetDsn == "" {
			// If still not provided, use default
			targetDsn = "postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"
			fmt.Println("Warning: Using default target database connection. Consider setting TARGET_DB_DSN environment variable or using -target-dsn flag.")
		}
	}

	// Connect to source database (SQL Server)
	sourceDb, err := sql.Open("sqlserver", sourceDsn)
	if err != nil {
		log.Fatalf("Error connecting to source database: %v", err)
	}
	defer sourceDb.Close()

	// Configure connection pool for source database
	sourceDb.SetMaxOpenConns(10)
	sourceDb.SetMaxIdleConns(5)
	sourceDb.SetConnMaxLifetime(time.Minute * 5)

	// Test source connection
	if err := sourceDb.Ping(); err != nil {
		log.Fatalf("Error connecting to source database: %v", err)
	}
	fmt.Println("✅ Connected to SQL Server source database")

	// Connect to target database (PostgreSQL)
	targetDb, err := sql.Open("postgres", targetDsn)
	if err != nil {
		log.Fatalf("Error connecting to target database: %v", err)
	}
	defer targetDb.Close()

	// Configure connection pool for target database
	targetDb.SetMaxOpenConns(10)
	targetDb.SetMaxIdleConns(5)
	targetDb.SetConnMaxLifetime(time.Minute * 5)

	// Test target connection
	if err := targetDb.Ping(); err != nil {
		log.Fatalf("Error connecting to target database: %v", err)
	}
	fmt.Println("✅ Connected to PostgreSQL target database")

	// Parse schemas flag
	schemas := strings.Split(*schemasFlag, ",")
	for i, schema := range schemas {
		schemas[i] = strings.TrimSpace(schema)
	}
	fmt.Printf("Including schemas: %s\n", strings.Join(schemas, ", "))

	// Get list of tables from source database
	tables, err := getSourceTables(sourceDb, schemas)
	if err != nil {
		log.Fatalf("Error getting tables: %v", err)
	}

	// Filter tables if specified
	if *tablesFlag != "" {
		includeTables := strings.Split(*tablesFlag, ",")
		filteredTables := make([]string, 0)
		for _, table := range tables {
			for _, includeTable := range includeTables {
				if strings.EqualFold(strings.TrimSpace(includeTable), table) {
					filteredTables = append(filteredTables, table)
					break
				}
			}
		}
		tables = filteredTables
	}

	// Exclude tables if specified
	if *excludeTablesFlag != "" {
		excludeTables := strings.Split(*excludeTablesFlag, ",")
		filteredTables := make([]string, 0)
		for _, table := range tables {
			exclude := false
			for _, excludeTable := range excludeTables {
				if strings.EqualFold(strings.TrimSpace(excludeTable), table) {
					exclude = true
					break
				}
			}
			if !exclude {
				filteredTables = append(filteredTables, table)
			}
		}
		tables = filteredTables
	}

	fmt.Printf("Found %d tables to migrate\n", len(tables))

	// Migrate each table
	startTime := time.Now()
	totalRows := 0

	for _, table := range tables {
		fmt.Printf("Migrating table: %s\n", table)

		// Get column information
		columns, err := getTableColumns(sourceDb, table)
		if err != nil {
			log.Fatalf("Error getting columns for table %s: %v", table, err)
		}

		// Truncate target table if specified
		if *truncateFlag {
			_, err := targetDb.Exec(fmt.Sprintf("TRUNCATE TABLE \"%s\"", table))
			if err != nil {
				log.Printf("Warning: Could not truncate table %s: %v", table, err)
			} else {
				fmt.Printf("Truncated table: %s\n", table)
			}
		}

		// Migrate data
		rowCount, err := migrateTableData(sourceDb, targetDb, table, columns, *batchSizeFlag)
		if err != nil {
			log.Fatalf("Error migrating data for table %s: %v", table, err)
		}

		totalRows += rowCount
		fmt.Printf("✅ Migrated %d rows from table: %s\n", rowCount, table)
	}

	duration := time.Since(startTime)
	fmt.Printf("\n✅ Migration completed in %s\n", duration)
	fmt.Printf("✅ Total rows migrated: %d\n", totalRows)
}

// getSourceTables returns a list of all tables in the source database
func getSourceTables(db *sql.DB, schemas []string) ([]string, error) {
	// Build schema filter for SQL queries
	schemaFilter := ""
	schemaParams := make([]interface{}, len(schemas))
	for i, schema := range schemas {
		if i > 0 {
			schemaFilter += " OR "
		}
		schemaFilter += "TABLE_SCHEMA = @p" + fmt.Sprintf("%d", i+1)
		schemaParams[i] = schema
	}

	query := fmt.Sprintf(`
		SELECT TABLE_SCHEMA, TABLE_NAME
		FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_TYPE = 'BASE TABLE'
		AND (%s)
		ORDER BY TABLE_SCHEMA, TABLE_NAME`, schemaFilter)

	rows, err := db.Query(query, schemaParams...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var schema, tableName string
		if err := rows.Scan(&schema, &tableName); err != nil {
			return nil, err
		}
		// Create a fully qualified table name with schema
		fullTableName := schema + "." + tableName
		tables = append(tables, fullTableName)
	}

	return tables, nil
}

// getTableColumns returns information about columns in the specified table
func getTableColumns(db *sql.DB, fullTableName string) ([]string, error) {
	// Split the full table name into schema and table
	parts := strings.Split(fullTableName, ".")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid table name format: %s (expected schema.table)", fullTableName)
	}
	schema := parts[0]
	table := parts[1]

	query := `
		SELECT COLUMN_NAME
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = @p1 AND TABLE_NAME = @p2
		ORDER BY ORDINAL_POSITION`

	rows, err := db.Query(query, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var columnName string
		if err := rows.Scan(&columnName); err != nil {
			return nil, err
		}
		columns = append(columns, columnName)
	}

	return columns, nil
}

// migrateTableData migrates data from the source table to the target table
func migrateTableData(sourceDb *sql.DB, targetDb *sql.DB, fullTableName string, columns []string, batchSize int) (int, error) {
	// Split the full table name into schema and table
	parts := strings.Split(fullTableName, ".")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid table name format: %s (expected schema.table)", fullTableName)
	}
	schema := parts[0]
	table := parts[1]

	// Build column list for queries
	columnList := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	sqlServerColumns := make([]string, len(columns))

	for i, col := range columns {
		// PostgreSQL uses double quotes for identifiers
		columnList[i] = fmt.Sprintf("\"%s\"", col)
		// SQL Server uses square brackets for identifiers
		sqlServerColumns[i] = fmt.Sprintf("[%s]", col)
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	// Prepare select query with properly escaped column names
	selectQuery := fmt.Sprintf("SELECT %s FROM [%s].[%s]", strings.Join(sqlServerColumns, ", "), schema, table)

	// Prepare insert query
	insertQuery := fmt.Sprintf(
		"INSERT INTO \"%s\" (%s) VALUES (%s)",
		fullTableName,
		strings.Join(columnList, ", "),
		strings.Join(placeholders, ", "),
	)

	// Execute select query
	rows, err := sourceDb.Query(selectQuery)
	if err != nil {
		return 0, fmt.Errorf("error querying source table: %v", err)
	}
	defer rows.Close()

	// Process rows in batches
	rowCount := 0
	batchCount := 0
	batchSize = 100 // Reduce batch size to avoid connection issues

	// Create a new transaction for each batch
	tx, err := targetDb.Begin()
	if err != nil {
		return 0, fmt.Errorf("error starting transaction: %v", err)
	}

	// Prepare statement within transaction
	stmt, err := tx.Prepare(insertQuery)
	if err != nil {
		tx.Rollback()
		return 0, fmt.Errorf("error preparing insert statement: %v", err)
	}
	defer stmt.Close()

	for rows.Next() {
		// Create a slice to hold the column values
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))

		// Create pointers to each element in the values slice
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		// Scan the row into the values slice
		if err := rows.Scan(valuePtrs...); err != nil {
			tx.Rollback()
			return rowCount, fmt.Errorf("error scanning row: %v", err)
		}

		// Execute insert statement
		_, err := stmt.Exec(values...)
		if err != nil {
			tx.Rollback()
			return rowCount, fmt.Errorf("error inserting row: %v", err)
		}

		rowCount++
		batchCount++

		// Commit transaction and start a new one after each batch
		if batchCount >= batchSize {
			if err := tx.Commit(); err != nil {
				return rowCount, fmt.Errorf("error committing transaction: %v", err)
			}

			fmt.Printf("  Migrated %d rows...\n", rowCount)

			// Start a new transaction and prepare a new statement
			tx, err = targetDb.Begin()
			if err != nil {
				return rowCount, fmt.Errorf("error starting transaction: %v", err)
			}

			// Close the previous statement and prepare a new one
			stmt.Close()
			stmt, err = tx.Prepare(insertQuery)
			if err != nil {
				tx.Rollback()
				return rowCount, fmt.Errorf("error preparing insert statement: %v", err)
			}

			batchCount = 0
		}
	}

	// Commit any remaining rows
	if batchCount > 0 {
		if err := tx.Commit(); err != nil {
			return rowCount, fmt.Errorf("error committing final transaction: %v", err)
		}
	} else {
		// If there were no rows in the last batch, rollback the empty transaction
		tx.Rollback()
	}

	return rowCount, nil
}
