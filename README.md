# dbmigrate

A tool for migrating SQL Server databases (schema and data) to PostgreSQL.

## Description

dbmigrate provides two main tools:

1. **Schema Migration**: Extracts schema information from a SQL Server database and generates a PostgreSQL-compatible schema file.
2. **Data Migration**: Transfers data from SQL Server tables to corresponding PostgreSQL tables.

The tools handle data type conversions, preserve primary key constraints, and provide progress feedback during migration.

## Tools

### 1. Schema Migration

The schema migration tool (`cmd/schema/main.go`) extracts schema information from a SQL Server database and generates a PostgreSQL-compatible schema file.

#### Usage

```bash
go run cmd/schema/main.go [options]
```

#### Options

- `-dsn string`: SQL Server connection string

#### Environment Variables

- `DB_DSN`: SQL Server connection string

#### Output

The tool generates a file named `postgres_schema.sql` containing the PostgreSQL-compatible schema definitions.

#### Example

```bash
# Using command line flag
go run cmd/schema/main.go -dsn "sqlserver://sa:StrongPassword@localhost:1433?database=AdventureWorks"

# Using environment variable
export DB_DSN="sqlserver://sa:StrongPassword@localhost:1433?database=AdventureWorks"
go run cmd/schema/main.go
```

### 2. Data Migration

The data migration tool (`cmd/migrate/main.go`) transfers data from SQL Server tables to corresponding PostgreSQL tables.

#### Usage

```bash
go run cmd/migrate/main.go [options]
```

#### Options

- `-source-dsn string`: SQL Server connection string
- `-target-dsn string`: PostgreSQL connection string
- `-batch-size int`: Number of rows to process in each batch (default: 1000)
- `-tables string`: Comma-separated list of tables to migrate (default: all)
- `-exclude-tables string`: Comma-separated list of tables to exclude from migration
- `-truncate`: Whether to truncate target tables before migration (default: false)

#### Environment Variables

- `SOURCE_DB_DSN`: SQL Server connection string
- `TARGET_DB_DSN`: PostgreSQL connection string

#### Example

```bash
# Basic usage
go run cmd/migrate/main.go -source-dsn "sqlserver://sa:StrongPassword@localhost:1433?database=AdventureWorks" \
                          -target-dsn "postgres://postgres:postgres@localhost:5432/adventureworks?sslmode=disable"

# Migrate specific tables with truncation
go run cmd/migrate/main.go -source-dsn "sqlserver://sa:StrongPassword@localhost:1433?database=AdventureWorks" \
                          -target-dsn "postgres://postgres:postgres@localhost:5432/adventureworks?sslmode=disable" \
                          -tables "Customer,Product,Order" \
                          -truncate

# Using environment variables
export SOURCE_DB_DSN="sqlserver://sa:StrongPassword@localhost:1433?database=AdventureWorks"
export TARGET_DB_DSN="postgres://postgres:postgres@localhost:5432/adventureworks?sslmode=disable"
go run cmd/migrate/main.go -batch-size 5000
```

## Connection String Formats

### SQL Server

```
sqlserver://username:password@host:port?database=dbname
```

### PostgreSQL

```
postgres://username:password@host:port/dbname?sslmode=disable
```

## Complete Migration Process

To perform a complete migration from SQL Server to PostgreSQL:

1. First, generate the PostgreSQL schema:
   ```bash
   go run cmd/schema/main.go -dsn "sqlserver://sa:pass@localhost:1433?database=yourdb"
   ```

2. Apply the generated schema to your PostgreSQL database:
   ```bash
   psql -U postgres -d yourdb -f postgres_schema.sql
   ```

3. Migrate the data:
   ```bash
   go run cmd/migrate/main.go -source-dsn "sqlserver://sa:pass@localhost:1433?database=yourdb" \
                             -target-dsn "postgres://postgres:pass@localhost:5432/yourdb?sslmode=disable"
   ```
