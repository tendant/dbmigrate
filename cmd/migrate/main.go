package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	_ "github.com/denisenkom/go-mssqldb"
	_ "github.com/lib/pq"
)

// redactPassword replaces the password in a connection string with "***" for logging
func redactPassword(dsn string) string {
	// Match password in connection string
	re := regexp.MustCompile(`(://[^:]+:)([^@]+)(@)`)
	return re.ReplaceAllString(dsn, "${1}***${3}")
}

// validateSqlServerDsn checks and fixes SQL Server connection string format
// It ensures the database is specified as a query parameter (?database=dbname)
// rather than as part of the path (/dbname)
func validateSqlServerDsn(dsn string) (string, bool) {
	// Check if there's a path component after the host:port
	hostAndPath := strings.Split(dsn, "://")
	if len(hostAndPath) != 2 {
		return dsn, false // Not a valid URL format
	}

	parts := strings.Split(hostAndPath[1], "/")
	if len(parts) <= 1 {
		return dsn, false // No path component, already correct
	}

	// Extract host part and database from path
	hostPart := parts[0]
	dbName := parts[1]

	// Check if there are query parameters
	queryParams := ""
	if strings.Contains(dbName, "?") {
		dbParts := strings.SplitN(dbName, "?", 2)
		dbName = dbParts[0]
		queryParams = "?" + dbParts[1]
	}

	// Build the corrected DSN with database as a query parameter
	correctedDsn := fmt.Sprintf("sqlserver://%s", hostPart)

	// Add database parameter
	if queryParams == "" {
		correctedDsn += fmt.Sprintf("?database=%s", dbName)
	} else {
		// Check if database parameter already exists
		if strings.Contains(queryParams, "database=") {
			correctedDsn += queryParams
		} else {
			correctedDsn += fmt.Sprintf("?database=%s&%s", dbName, queryParams[1:])
		}
	}

	return correctedDsn, true
}

func main() {
	// Define command line flags
	// Database connection flags
	sourceDsnFlag := flag.String("source-dsn", "", "SQL Server connection string (e.g., sqlserver://user:pass@host:1433?database=yourdb)")
	targetDsnFlag := flag.String("target-dsn", "", "PostgreSQL connection string (e.g., postgres://user:pass@host:5432/dbname?sslmode=disable)")

	// Table selection flags
	tablesFlag := flag.String("tables", "", "Comma-separated list of tables to migrate (default: all)")
	excludeTablesFlag := flag.String("exclude-tables", "", "Comma-separated list of tables to exclude from migration (supports wildcards with '*')")
	excludeEmptyTablesFlag := flag.Bool("exclude-empty-tables", false, "Skip tables with no rows")
	excludeLargeTablesFlag := flag.Int("exclude-large-tables", 0, "Skip tables with more rows than this value (0 = no limit)")
	maxTableSizeFlag := flag.Int64("max-table-size", 0, "Skip tables larger than this size in MB (0 = no limit)")
	skipIfExistsFlag := flag.Bool("skip-if-exists", false, "Skip migration if the target table already has data")
	schemasFlag := flag.String("schemas", "dbo", "Comma-separated list of schemas to include (default: dbo)")

	// Performance flags
	batchSizeFlag := flag.Int("batch-size", 1000, "Number of rows to process in each batch")

	// Behavior flags
	truncateFlag := flag.Bool("truncate", false, "Whether to truncate target tables before migration")
	debugFlag := flag.Bool("debug", false, "Enable debug logging")
	includeSystemSchemasFlag := flag.Bool("include-system-schemas", false, "Include system schemas in migration (default: false)")
	preserveCaseFlag := flag.Bool("preserve-case", false, "Preserve case sensitivity of identifiers using double quotes (default: false)")
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

	// Check if the database is specified correctly (as a query parameter, not in the path)
	if correctedDsn, wasFixed := validateSqlServerDsn(sourceDsn); wasFixed {
		fmt.Printf("Warning: Incorrect database format in connection string. The database should be specified as a query parameter.\n")
		fmt.Printf("Original: %s\n", redactPassword(sourceDsn))
		fmt.Printf("Corrected: %s\n", redactPassword(correctedDsn))
		sourceDsn = correctedDsn
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

	// Redact password from DSN for logging
	redactedDsn := redactPassword(sourceDsn)
	fmt.Printf("Connecting to SQL Server source with DSN: %s\n", redactedDsn)

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

	// Get current database name
	var dbName string
	err = sourceDb.QueryRow("SELECT DB_NAME()").Scan(&dbName)
	if err != nil {
		log.Printf("Warning: Could not determine current database name: %v", err)
	} else {
		fmt.Printf("Connected to database: %s\n", dbName)
	}

	// Get total table count for verification
	var totalTableCount int
	err = sourceDb.QueryRow(`
		SELECT COUNT(*) 
		FROM INFORMATION_SCHEMA.TABLES 
		WHERE TABLE_TYPE = 'BASE TABLE'`).Scan(&totalTableCount)
	if err != nil {
		log.Printf("Warning: Could not get total table count: %v", err)
	} else {
		fmt.Printf("Total tables in database: %d\n", totalTableCount)
	}

	// List all available schemas in the database with table counts
	fmt.Println("Listing available schemas in the source database:")
	schemasQuery := `
		SELECT TABLE_SCHEMA AS schema_name, COUNT(*) AS table_count
		FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_TYPE = 'BASE TABLE'
		GROUP BY TABLE_SCHEMA
		ORDER BY table_count DESC, schema_name`

	if *debugFlag {
		fmt.Printf("Executing schema query: %s\n", schemasQuery)
	}

	schemaRows, err := sourceDb.Query(schemasQuery)
	if err != nil {
		log.Printf("Error executing schema query: %v", err)
		// Try a simpler query as fallback
		fmt.Println("Trying fallback query...")
		fallbackQuery := "SELECT DISTINCT TABLE_SCHEMA FROM INFORMATION_SCHEMA.TABLES"
		fallbackRows, fallbackErr := sourceDb.Query(fallbackQuery)
		if fallbackErr != nil {
			log.Printf("Error executing fallback query: %v", fallbackErr)
		} else {
			defer fallbackRows.Close()
			fmt.Println("Available schemas (fallback method):")
			for fallbackRows.Next() {
				var schemaName string
				if err := fallbackRows.Scan(&schemaName); err != nil {
					log.Printf("Error scanning schema name: %v", err)
					continue
				}
				fmt.Printf("  - %s\n", schemaName)
			}
		}
	} else {
		var availableSchemas []string
		schemaTableCounts := make(map[string]int)

		for schemaRows.Next() {
			var schemaName string
			var tableCount int
			if err := schemaRows.Scan(&schemaName, &tableCount); err != nil {
				log.Printf("Warning: Error scanning schema information: %v", err)
				continue
			}
			availableSchemas = append(availableSchemas, schemaName)
			schemaTableCounts[schemaName] = tableCount
		}
		schemaRows.Close()

		if len(availableSchemas) > 0 {
			fmt.Println("Available schemas:")
			totalTables := 0
			for _, schema := range availableSchemas {
				tableCount := schemaTableCounts[schema]
				totalTables += tableCount
				fmt.Printf("  - %s (%d tables)\n", schema, tableCount)
			}
			fmt.Printf("Total: %d schemas, %d tables\n", len(availableSchemas), totalTables)
		} else {
			fmt.Println("No schemas found or could not retrieve schema information.")
		}
	}

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

	// Define system schemas to exclude
	systemSchemas := map[string]bool{
		"sys":                true,
		"INFORMATION_SCHEMA": true,
		"db_owner":           true,
		"db_accessadmin":     true,
		"db_securityadmin":   true,
		"db_ddladmin":        true,
		"db_backupoperator":  true,
		"db_datareader":      true,
		"db_datawriter":      true,
		"db_denydatareader":  true,
		"db_denydatawriter":  true,
	}

	// Filter out system schemas if not explicitly included
	if !*includeSystemSchemasFlag {
		filteredSchemas := make([]string, 0)
		for _, schema := range schemas {
			if !systemSchemas[schema] {
				filteredSchemas = append(filteredSchemas, schema)
			}
		}

		if len(filteredSchemas) == 0 {
			log.Println("Warning: All specified schemas are system schemas. If you want to include system schemas, use the -include-system-schemas flag.")
			// Default to dbo if all specified schemas are system schemas
			filteredSchemas = []string{"dbo"}
		}

		schemas = filteredSchemas
		fmt.Printf("After filtering system schemas: %s\n", strings.Join(schemas, ", "))
	}

	// Get list of tables from source database
	tables, err := getSourceTables(sourceDb, schemas)
	if err != nil {
		log.Fatalf("Error getting tables: %v", err)
	}

	// Filter out system tables (tables with names starting with "sys")
	if !*includeSystemSchemasFlag {
		filteredTables := make([]string, 0)
		for _, table := range tables {
			parts := strings.Split(table, ".")
			if len(parts) == 2 {
				tableName := parts[1]
				if !strings.HasPrefix(strings.ToLower(tableName), "sys") {
					filteredTables = append(filteredTables, table)
				} else {
					fmt.Printf("Excluding system table: %s\n", table)
				}
			} else {
				filteredTables = append(filteredTables, table)
			}
		}
		tables = filteredTables
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
			for _, excludePattern := range excludeTables {
				pattern := strings.TrimSpace(excludePattern)
				// Support wildcard matching
				if strings.Contains(pattern, "*") {
					// Convert wildcard pattern to regex
					regexPattern := "^" + strings.ReplaceAll(pattern, "*", ".*") + "$"
					match, err := regexp.MatchString(regexPattern, table)
					if err == nil && match {
						exclude = true
						fmt.Printf("Excluding table (wildcard match): %s\n", table)
						break
					}
				} else if strings.EqualFold(pattern, table) {
					exclude = true
					fmt.Printf("Excluding table (exact match): %s\n", table)
					break
				}
			}
			if !exclude {
				filteredTables = append(filteredTables, table)
			}
		}
		tables = filteredTables
	}

	// Skip empty tables if specified
	if *excludeEmptyTablesFlag {
		filteredTables := make([]string, 0)
		for _, table := range tables {
			// Check if table is empty
			parts := strings.Split(table, ".")
			if len(parts) != 2 {
				filteredTables = append(filteredTables, table)
				continue
			}
			schema := parts[0]
			tableName := parts[1]

			var rowCount int
			countQuery := fmt.Sprintf("SELECT COUNT(1) FROM [%s].[%s]", schema, tableName)
			err := sourceDb.QueryRow(countQuery).Scan(&rowCount)
			if err != nil {
				log.Printf("Warning: Could not get row count for table %s: %v", table, err)
				filteredTables = append(filteredTables, table)
				continue
			}

			if rowCount > 0 {
				filteredTables = append(filteredTables, table)
			} else {
				fmt.Printf("Skipping empty table: %s\n", table)
			}
		}
		tables = filteredTables
	}

	// Skip large tables if specified
	if *excludeLargeTablesFlag > 0 {
		filteredTables := make([]string, 0)
		for _, table := range tables {
			// Check if table has more rows than the threshold
			parts := strings.Split(table, ".")
			if len(parts) != 2 {
				filteredTables = append(filteredTables, table)
				continue
			}
			schema := parts[0]
			tableName := parts[1]

			var rowCount int
			countQuery := fmt.Sprintf("SELECT COUNT(1) FROM [%s].[%s]", schema, tableName)
			err := sourceDb.QueryRow(countQuery).Scan(&rowCount)
			if err != nil {
				log.Printf("Warning: Could not get row count for table %s: %v", table, err)
				filteredTables = append(filteredTables, table)
				continue
			}

			if rowCount <= *excludeLargeTablesFlag {
				filteredTables = append(filteredTables, table)
			} else {
				fmt.Printf("Skipping large table: %s (%d rows > threshold of %d)\n",
					table, rowCount, *excludeLargeTablesFlag)
			}
		}
		tables = filteredTables
	}

	// Skip tables larger than max size if specified
	if *maxTableSizeFlag > 0 {
		filteredTables := make([]string, 0)
		for _, table := range tables {
			// Check if table is larger than the threshold
			parts := strings.Split(table, ".")
			if len(parts) != 2 {
				filteredTables = append(filteredTables, table)
				continue
			}
			schema := parts[0]
			tableName := parts[1]

			// This is a rough estimate of table size based on SQL Server's sys.dm_db_partition_stats
			sizeQuery := fmt.Sprintf(`
				SELECT SUM(used_page_count) * 8 / 1024 AS size_mb
				FROM sys.dm_db_partition_stats
				WHERE object_id = OBJECT_ID('[%s].[%s]')
			`, schema, tableName)

			var sizeInMB int64
			err := sourceDb.QueryRow(sizeQuery).Scan(&sizeInMB)
			if err != nil {
				log.Printf("Warning: Could not get size for table %s: %v", table, err)
				filteredTables = append(filteredTables, table)
				continue
			}

			if sizeInMB <= *maxTableSizeFlag {
				filteredTables = append(filteredTables, table)
			} else {
				fmt.Printf("Skipping large table: %s (%d MB > threshold of %d MB)\n",
					table, sizeInMB, *maxTableSizeFlag)
			}
		}
		tables = filteredTables
	}

	// Skip tables that already have data in the target database
	if *skipIfExistsFlag {
		filteredTables := make([]string, 0)
		for _, table := range tables {
			// Check if target table already has data
			parts := strings.Split(table, ".")
			if len(parts) != 2 {
				filteredTables = append(filteredTables, table)
				continue
			}
			schema := parts[0]
			tableName := parts[1]

			var countQuery string
			if *preserveCaseFlag {
				countQuery = fmt.Sprintf("SELECT COUNT(1) FROM \"%s\".\"%s\" LIMIT 1", schema, tableName)
			} else {
				countQuery = fmt.Sprintf("SELECT COUNT(1) FROM %s.%s LIMIT 1", schema, tableName)
			}

			var rowCount int
			err := targetDb.QueryRow(countQuery).Scan(&rowCount)
			if err != nil {
				// If there's an error (e.g., table doesn't exist), include the table
				filteredTables = append(filteredTables, table)
				continue
			}

			if rowCount == 0 {
				filteredTables = append(filteredTables, table)
			} else {
				fmt.Printf("Skipping table with existing data: %s\n", table)
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
			// Split the full table name into schema and table
			parts := strings.Split(table, ".")
			if len(parts) != 2 {
				log.Printf("Warning: Invalid table name format: %s (expected schema.table)", table)
				continue
			}
			schema := parts[0]
			tableName := parts[1]

			var truncateSQL string
			if *preserveCaseFlag {
				truncateSQL = fmt.Sprintf("TRUNCATE TABLE \"%s\".\"%s\"", schema, tableName)
			} else {
				truncateSQL = fmt.Sprintf("TRUNCATE TABLE %s.%s", schema, tableName)
			}
			_, err := targetDb.Exec(truncateSQL)
			if err != nil {
				log.Printf("Warning: Could not truncate table %s: %v", table, err)
			} else {
				fmt.Printf("Truncated table: %s\n", table)
			}
		}

		// Migrate data
		rowCount, err := migrateTableData(sourceDb, targetDb, table, columns, *batchSizeFlag, *preserveCaseFlag)
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
		SELECT TABLE_SCHEMA AS schema_name, TABLE_NAME AS table_name
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
func migrateTableData(sourceDb *sql.DB, targetDb *sql.DB, fullTableName string, columns []string, batchSize int, preserveCase bool) (int, error) {
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
		// Format PostgreSQL column names based on preserve-case flag
		if preserveCase {
			columnList[i] = fmt.Sprintf("\"%s\"", col)
		} else {
			columnList[i] = col
		}
		// SQL Server uses square brackets for identifiers
		sqlServerColumns[i] = fmt.Sprintf("[%s]", col)
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}

	// Prepare select query with properly escaped column names
	selectQuery := fmt.Sprintf("SELECT %s FROM [%s].[%s]", strings.Join(sqlServerColumns, ", "), schema, table)

	// Prepare insert query with schema-qualified table name
	var insertQuery string
	if preserveCase {
		insertQuery = fmt.Sprintf(
			"INSERT INTO \"%s\".\"%s\" (%s) VALUES (%s)",
			schema,
			table,
			strings.Join(columnList, ", "),
			strings.Join(placeholders, ", "),
		)
	} else {
		insertQuery = fmt.Sprintf(
			"INSERT INTO %s.%s (%s) VALUES (%s)",
			schema,
			table,
			strings.Join(columnList, ", "),
			strings.Join(placeholders, ", "),
		)
	}

	// Execute select query
	rows, err := sourceDb.Query(selectQuery)
	if err != nil {
		return 0, fmt.Errorf("error querying source table: %v", err)
	}
	defer rows.Close()

	// Process rows in batches using the user-specified batch size
	rowCount := 0
	batchCount := 0
	// The batch size controls how many rows are processed in a single transaction
	fmt.Printf("Using batch size: %d rows per transaction\n", batchSize)

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
