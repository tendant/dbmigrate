package main

import (
	"database/sql"
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
	dsn := "sqlserver://user:pass@host:1433?database=yourdb"
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
			columns = append(columns, fmt.Sprintf("  PRIMARY KEY (%s)", strings.Join(pks, ", ")))
		}
		schema := fmt.Sprintf("CREATE TABLE \"%s\" (\n%s\n);\n\n", table, strings.Join(columns, ",\n"))
		fmt.Print(schema)        // print to console
		file.WriteString(schema) // write to file
	}

	fmt.Println("✅ PostgreSQL schema written to postgres_schema.sql")
}
