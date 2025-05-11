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
	// Define command line flag for database connection string
	dsnFlag := flag.String("dsn", "", "Database connection string (e.g., sqlserver://user:pass@host:1433?database=yourdb)")
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

	// Ensure the connection string has the required parameters for SQL Server
	// This is especially important for AWS RDS instances
	if !strings.Contains(dsn, "connection timeout") {
		// Add connection timeout if not present
		if strings.Contains(dsn, "?") {
			dsn += "&connection timeout=30"
		} else {
			dsn += "?connection timeout=30"
		}
	}

	// Disable SQL Server Browser service lookup for AWS RDS instances
	if strings.Contains(dsn, "rds.amazonaws.com") && !strings.Contains(dsn, "server sni") {
		// Add server SNI parameter for AWS RDS instances
		dsn += "&server sni=disable"
	}

	fmt.Printf("Connecting to SQL Server with DSN: %s\n", dsn)
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	columnQuery := `
		SELECT TABLE_NAME, COLUMN_NAME, DATA_TYPE, IS_NULLABLE
		FROM INFORMATION_SCHEMA.COLUMNS
		ORDER BY TABLE_NAME, ORDINAL_POSITION`

	pkQuery := `
		SELECT t.name AS table_name, c.name AS column_name
		FROM sys.indexes i
		JOIN sys.index_columns ic ON i.object_id = ic.object_id AND i.index_id = ic.index_id
		JOIN sys.columns c ON ic.object_id = c.object_id AND ic.column_id = c.column_id
		JOIN sys.tables t ON i.object_id = t.object_id
		WHERE i.is_primary_key = 1`

	rows, err := db.Query(columnQuery)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	tables := make(map[string][]string)
	for rows.Next() {
		var table, column, dataType, nullable string
		if err := rows.Scan(&table, &column, &dataType, &nullable); err != nil {
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

		colDef := fmt.Sprintf("  \"%s\" %s %s", column, pgType, null)
		tables[table] = append(tables[table], colDef)
	}

	// Get primary key columns
	pkRows, err := db.Query(pkQuery)
	if err != nil {
		log.Fatal(err)
	}
	defer pkRows.Close()

	pkMap := make(map[string][]string)
	for pkRows.Next() {
		var table, column string
		if err := pkRows.Scan(&table, &column); err != nil {
			log.Fatal(err)
		}
		pkMap[table] = append(pkMap[table], column)
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
