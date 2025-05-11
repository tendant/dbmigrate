package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"

	_ "github.com/denisenkom/go-mssqldb"
)

// redactPassword replaces the password in a connection string with "***" for logging
func redactPassword(dsn string) string {
	// Match password in connection string
	re := regexp.MustCompile(`(://[^:]+:)([^@]+)(@)`)
	return re.ReplaceAllString(dsn, "${1}***${3}")
}

var typeMapping = map[string]string{
	"int":              "INTEGER",
	"bigint":           "BIGINT",
	"smallint":         "SMALLINT",
	"bit":              "BOOLEAN",
	"nvarchar":         "TEXT",
	"varchar":          "TEXT",
	"nchar":            "TEXT",
	"char":             "TEXT",
	"text":             "TEXT",
	"datetime":         "TIMESTAMPTZ",
	"datetime2":        "TIMESTAMPTZ",
	"date":             "DATE",
	"float":            "DOUBLE PRECISION",
	"real":             "REAL",
	"decimal":          "NUMERIC",
	"numeric":          "NUMERIC",
	"money":            "NUMERIC",
	"uniqueidentifier": "UUID",
}

func main() {
	// Define command line flags
	dsnFlag := flag.String("dsn", "", "Database connection string (e.g., sqlserver://user:pass@host:1433?database=yourdb)")
	schemasFlag := flag.String("schemas", "dbo", "Comma-separated list of schemas to include (default: dbo)")
	debugFlag := flag.Bool("debug", false, "Enable debug logging")
	includeSystemSchemasFlag := flag.Bool("include-system-schemas", false, "Include system schemas in migration (default: false)")
	preserveCaseFlag := flag.Bool("preserve-case", false, "Preserve case sensitivity of identifiers using double quotes (default: false)")
	flag.Parse()

	// Determine the DSN to use (command line arg -> environment variable -> default)
	dsn := *dsnFlag
	if dsn == "" {
		// If not provided via command line, check environment variable
		dsn = os.Getenv("DB_DSN")
		if dsn == "" {
			// If still not provided, use default
			dsn = "sqlserver://user:pass@host:1433?database=yourdb"
			fmt.Println("Warning: Using default database connection. Consider setting DB_DSN environment variable or using -dsn flag.")
		}
	}

	// Check if the protocol is correct (should be sqlserver:// not mssql://)
	if strings.HasPrefix(dsn, "mssql://") {
		dsn = "sqlserver://" + dsn[8:]
		fmt.Println("Warning: Changed protocol from mssql:// to sqlserver://")
	}

	// Ensure the connection string has the required parameters for SQL Server
	// This is especially important for AWS RDS instances

	// Start with the base DSN
	dsnParams := make(map[string]string)

	// Parse existing parameters
	if strings.Contains(dsn, "?") {
		parts := strings.SplitN(dsn, "?", 2)
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
		dsn = baseURL
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
	if strings.Contains(dsn, "rds.amazonaws.com") {
		if _, ok := dsnParams["server sni"]; !ok {
			dsnParams["server sni"] = "disable"
		}
		// Ensure we're using the host from the connection string, not localhost
		if _, ok := dsnParams["server"]; !ok {
			// Extract host from connection string
			host := ""
			if strings.Contains(dsn, "@") {
				parts := strings.Split(dsn, "@")
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
		dsn += "?"
		paramStrings := make([]string, 0, len(dsnParams))
		for key, value := range dsnParams {
			paramStrings = append(paramStrings, key+"="+value)
		}
		dsn += strings.Join(paramStrings, "&")
	}

	// Parse schemas flag
	schemas := strings.Split(*schemasFlag, ",")
	for i, schema := range schemas {
		schemas[i] = strings.TrimSpace(schema)
	}
	fmt.Printf("Including schemas: %s\n", strings.Join(schemas, ", "))

	// Redact password from DSN for logging
	redactedDsn := redactPassword(dsn)
	fmt.Printf("Connecting to SQL Server with DSN: %s\n", redactedDsn)

	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		log.Fatalf("Error opening database connection: %v", err)
	}
	defer db.Close()

	// Get current database name
	var dbName string
	err = db.QueryRow("SELECT DB_NAME()").Scan(&dbName)
	if err != nil {
		log.Printf("Warning: Could not determine current database name: %v", err)
	} else {
		fmt.Printf("Connected to database: %s\n", dbName)
	}

	// Get total table count for verification
	var totalTableCount int
	err = db.QueryRow(`
		SELECT COUNT(*) 
		FROM INFORMATION_SCHEMA.TABLES 
		WHERE TABLE_TYPE = 'BASE TABLE'`).Scan(&totalTableCount)
	if err != nil {
		log.Printf("Warning: Could not get total table count: %v", err)
	} else {
		fmt.Printf("Total tables in database: %d\n", totalTableCount)
	}

	// List all available schemas in the database with table counts
	fmt.Println("Listing available schemas in the database:")
	schemasQuery := `
		SELECT TABLE_SCHEMA AS schema_name, COUNT(*) AS table_count
		FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_TYPE = 'BASE TABLE'
		GROUP BY TABLE_SCHEMA
		ORDER BY table_count DESC, schema_name`

	if *debugFlag {
		fmt.Printf("Executing schema query: %s\n", schemasQuery)
	}

	schemaRows, err := db.Query(schemasQuery)
	if err != nil {
		log.Printf("Error executing schema query: %v", err)
		// Try a simpler query as fallback
		fmt.Println("Trying fallback query...")
		fallbackQuery := "SELECT DISTINCT TABLE_SCHEMA FROM INFORMATION_SCHEMA.TABLES"
		fallbackRows, fallbackErr := db.Query(fallbackQuery)
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

	columnQuery := fmt.Sprintf(`
		SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, DATA_TYPE, IS_NULLABLE
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE %s
		ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION`, schemaFilter)

	// Build schema filter for primary key query
	schemaPKFilter := ""
	for i, schema := range schemas {
		if i > 0 {
			schemaPKFilter += " OR "
		}
		schemaPKFilter += "s.name = '" + schema + "'"
	}

	pkQuery := fmt.Sprintf(`
		SELECT s.name AS schema_name, t.name AS table_name, c.name AS column_name
		FROM sys.indexes i
		JOIN sys.index_columns ic ON i.object_id = ic.object_id AND i.index_id = ic.index_id
		JOIN sys.columns c ON ic.object_id = c.object_id AND ic.column_id = c.column_id
		JOIN sys.tables t ON i.object_id = t.object_id
		JOIN sys.schemas s ON t.schema_id = s.schema_id
		WHERE i.is_primary_key = 1
		AND (%s)`, schemaPKFilter)

	rows, err := db.Query(columnQuery, schemaParams...)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	tables := make(map[string][]string)
	schemaTableMap := make(map[string]string) // Maps full table name to schema.table format

	for rows.Next() {
		var schema, table, column, dataType, nullable string
		if err := rows.Scan(&schema, &table, &column, &dataType, &nullable); err != nil {
			log.Fatal(err)
		}

		pgType, ok := typeMapping[strings.ToLower(dataType)]
		if !ok {
			pgType = "TEXT"
		}

		null := "NOT NULL"
		if nullable == "YES" {
			null = "NULL"
		}

		// Create a unique table identifier that includes the schema
		tableKey := schema + "." + table
		schemaTableMap[table] = tableKey

		// Format column definition based on preserve-case flag
		var colDef string
		if *preserveCaseFlag {
			colDef = fmt.Sprintf("  \"%s\" %s %s", column, pgType, null)
		} else {
			colDef = fmt.Sprintf("  %s %s %s", column, pgType, null)
		}
		tables[tableKey] = append(tables[tableKey], colDef)
	}

	// Get primary key columns
	pkRows, err := db.Query(pkQuery)
	if err != nil {
		log.Fatal(err)
	}
	defer pkRows.Close()

	pkMap := make(map[string][]string)
	for pkRows.Next() {
		var schema, table, column string
		if err := pkRows.Scan(&schema, &table, &column); err != nil {
			log.Fatal(err)
		}
		tableKey := schema + "." + table
		pkMap[tableKey] = append(pkMap[tableKey], column)
	}

	// Filter out system tables (tables with names starting with "sys")
	if !*includeSystemSchemasFlag {
		for tableKey := range tables {
			parts := strings.Split(tableKey, ".")
			if len(parts) == 2 {
				tableName := parts[1]
				if strings.HasPrefix(strings.ToLower(tableName), "sys") {
					fmt.Printf("Excluding system table: %s\n", tableKey)
					delete(tables, tableKey)
				}
			}
		}
	}

	// Write schema to file
	file, err := os.Create("postgres_schema.sql")
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	tableNames := make([]string, 0, len(tables))
	for name := range tables {
		tableNames = append(tableNames, name)
	}
	sort.Strings(tableNames)

	// Create a map to track which schemas we've created
	createdSchemas := make(map[string]bool)

	for _, table := range tableNames {
		columns := tables[table]
		if pks, ok := pkMap[table]; ok && len(pks) > 0 {
			// Format primary key based on preserve-case flag
			if *preserveCaseFlag {
				// Quote each primary key column name
				quotedPKs := make([]string, len(pks))
				for i, pk := range pks {
					quotedPKs[i] = fmt.Sprintf("\"%s\"", pk)
				}
				columns = append(columns, fmt.Sprintf("  PRIMARY KEY (%s)", strings.Join(quotedPKs, ", ")))
			} else {
				// Use primary key column names without quotes
				columns = append(columns, fmt.Sprintf("  PRIMARY KEY (%s)", strings.Join(pks, ", ")))
			}
		}

		var schemaSQL string

		// Check if table name contains a schema prefix
		if strings.Contains(table, ".") {
			// Case 1: Schema specified (e.g., "dbo.Users")
			parts := strings.Split(table, ".")
			schemaName := parts[0]
			tableName := parts[1]

			// Create schema if it doesn't exist and we haven't created it yet
			if !createdSchemas[schemaName] {
				var createSchema string
				if *preserveCaseFlag {
					createSchema = fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS \"%s\";\n\n", schemaName)
				} else {
					createSchema = fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s;\n\n", schemaName)
				}
				file.WriteString(createSchema) // write to file
				fmt.Print(createSchema)        // print to console
				createdSchemas[schemaName] = true
			}

			// Create table in the specified schema
			if *preserveCaseFlag {
				schemaSQL = fmt.Sprintf("CREATE TABLE \"%s\".\"%s\" (\n%s\n);\n\n",
					schemaName, tableName, strings.Join(columns, ",\n"))
			} else {
				schemaSQL = fmt.Sprintf("CREATE TABLE %s.%s (\n%s\n);\n\n",
					schemaName, tableName, strings.Join(columns, ",\n"))
			}
		} else {
			// Case 2: No schema specified, use public schema
			if *preserveCaseFlag {
				schemaSQL = fmt.Sprintf("CREATE TABLE \"%s\" (\n%s\n);\n\n",
					table, strings.Join(columns, ",\n"))
			} else {
				schemaSQL = fmt.Sprintf("CREATE TABLE %s (\n%s\n);\n\n",
					table, strings.Join(columns, ",\n"))
			}
		}

		fmt.Print(schemaSQL)        // print to console
		file.WriteString(schemaSQL) // write to file
	}

	fmt.Println("✅ PostgreSQL schema written to postgres_schema.sql")
}
