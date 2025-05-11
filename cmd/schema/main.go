package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	_ "github.com/denisenkom/go-mssqldb"
)

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

	fmt.Printf("Connecting to SQL Server with DSN: %s\n", dsn)
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

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

		colDef := fmt.Sprintf("  \"%s\" %s %s", column, pgType, null)
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

	for _, table := range tableNames {
		columns := tables[table]
		if pks, ok := pkMap[table]; ok && len(pks) > 0 {
			// Quote each primary key column name
			quotedPKs := make([]string, len(pks))
			for i, pk := range pks {
				quotedPKs[i] = fmt.Sprintf("\"%s\"", pk)
			}
			columns = append(columns, fmt.Sprintf("  PRIMARY KEY (%s)", strings.Join(quotedPKs, ", ")))
		}
		schema := fmt.Sprintf("CREATE TABLE \"%s\" (\n%s\n);\n\n", table, strings.Join(columns, ",\n"))
		fmt.Print(schema)        // print to console
		file.WriteString(schema) // write to file
	}

	fmt.Println("✅ PostgreSQL schema written to postgres_schema.sql")
}
