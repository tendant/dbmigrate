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
- `-schemas string`: Comma-separated list of schemas to include (default: "dbo")

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
- `-schemas string`: Comma-separated list of schemas to include (default: "dbo")
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

For AWS RDS SQL Server instances, use:

```
sqlserver://username:password@your-instance.rds.amazonaws.com:1433?database=dbname
```

**Important Notes:**
- Always use `sqlserver://` protocol (not `mssql://`). The tools will automatically correct this if needed.
- The tools will automatically add necessary parameters for AWS RDS instances, including:
  - `connection timeout=30` - Sets a connection timeout to prevent hanging
  - `encrypt=disable` - Disables encryption which can cause issues with AWS RDS
  - `server sni=disable` - Disables Server Name Indication
  - `browser=disable` - Disables SQL Server Browser service lookup (port 1434)
  - `dial timeout=10` - Sets a dial timeout to prevent hanging
  - `server=hostname` - Explicitly sets the server hostname to prevent localhost resolution issues

### Handling Special Characters in Passwords

If your database password contains special characters, you need to URL-encode those characters in the connection string. Here are some common special characters and their URL-encoded equivalents:

| Character | URL-Encoded |
|-----------|-------------|
| `@`       | `%40`       |
| `:`       | `%3A`       |
| `/`       | `%2F`       |
| `?`       | `%3F`       |
| `#`       | `%23`       |
| `&`       | `%26`       |
| `=`       | `%3D`       |
| `+`       | `%2B`       |
| `$`       | `%24`       |
| `%`       | `%25`       |
| ` ` (space) | `%20`     |

For example, if your password is `p@ssw0rd!`, you would use `p%40ssw0rd%21` in the connection string:

```
sqlserver://username:p%40ssw0rd%21@host:1433?database=dbname
```

You can use online URL encoders or the following command to encode your password:

```bash
# For Linux/macOS
echo -n "your_password" | python -c "import sys, urllib.parse; print(urllib.parse.quote(sys.stdin.read()))"

# For Windows PowerShell
[System.Web.HttpUtility]::UrlEncode("your_password")
```

### PostgreSQL

```
postgres://username:password@host:port/dbname?sslmode=disable
```

## Working with Multiple Schemas

Both tools support working with multiple schemas in SQL Server. By default, they only include tables from the "dbo" schema, but you can specify multiple schemas using the `-schemas` flag.

### Schema Discovery

When you run either tool, it will automatically list all available schemas in the database after connecting, along with the number of tables in each schema:

```
Listing available schemas in the database:
Available schemas:
  - db_accessadmin (0 tables)
  - db_backupoperator (0 tables)
  - db_datareader (0 tables)
  - db_datawriter (0 tables)
  - db_ddladmin (0 tables)
  - db_denydatareader (0 tables)
  - db_denydatawriter (0 tables)
  - db_owner (0 tables)
  - db_securityadmin (0 tables)
  - dbo (42 tables)
  - guest (0 tables)
  - INFORMATION_SCHEMA (0 tables)
  - sys (0 tables)
Total: 13 schemas, 42 tables
```

This helps you identify which schemas are available and their size before deciding which ones to include in your migration. The tools use system catalog views (`sys.schemas` and `sys.tables`) to provide accurate table counts for each schema, including system schemas. Schemas are displayed in descending order by table count, so you can easily identify the most data-rich schemas.

### Specifying Schemas to Include

You can specify which schemas to include using the `-schemas` flag:

```bash
# Include tables from multiple schemas
go run cmd/schema/main.go -dsn "sqlserver://user:pass@host:1433?database=yourdb" -schemas "dbo,sales,hr"

# Migrate data from multiple schemas
go run cmd/migrate/main.go -source-dsn "sqlserver://user:pass@host:1433?database=yourdb" \
                          -target-dsn "postgres://postgres:pass@localhost:5432/yourdb?sslmode=disable" \
                          -schemas "dbo,sales,hr"
```

The generated PostgreSQL schema and data migration will preserve the schema structure by creating tables with schema-qualified names (e.g., `"dbo"."Users"`, `"sales"."Orders"`).

## Complete Migration Process

To perform a complete migration from SQL Server to PostgreSQL:

1. First, generate the PostgreSQL schema:
   ```bash
   go run cmd/schema/main.go -dsn "sqlserver://sa:pass@localhost:1433?database=yourdb" -schemas "dbo,sales,hr"
   ```

2. Apply the generated schema to your PostgreSQL database:
   ```bash
   psql -U postgres -d yourdb -f postgres_schema.sql
   ```

3. Migrate the data:
   ```bash
   go run cmd/migrate/main.go -source-dsn "sqlserver://sa:pass@localhost:1433?database=yourdb" \
                             -target-dsn "postgres://postgres:pass@localhost:5432/yourdb?sslmode=disable" \
                             -schemas "dbo,sales,hr"
   ```
