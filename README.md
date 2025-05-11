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
- `-include-system-schemas`: Include system schemas in migration (default: false)
- `-preserve-case`: Preserve case sensitivity of identifiers using double quotes (default: false)
- `-debug`: Enable debug logging

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

# Preserving case sensitivity
go run cmd/schema/main.go -dsn "sqlserver://sa:StrongPassword@localhost:1433?database=AdventureWorks" -preserve-case
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
- `-batch-size int`: Number of rows to process in each batch (default: 1000). This value is fully customizable and will be respected by the migration process.
#### Table Selection Options
- `-tables string`: Comma-separated list of tables to migrate (default: all)
- `-exclude-tables string`: Comma-separated list of tables to exclude from migration (supports wildcards with '*')
- `-exclude-empty-tables`: Skip tables with no rows
- `-exclude-large-tables int`: Skip tables with more rows than this value (0 = no limit)
- `-max-table-size int`: Skip tables larger than this size in MB (0 = no limit)
- `-skip-if-exists`: Skip migration if the target table already has data
- `-schemas string`: Comma-separated list of schemas to include (default: "dbo")
- `-include-system-schemas`: Include system schemas in migration (default: false)

#### Performance Options
- `-batch-size int`: Number of rows to process in each batch (default: 1000). This value is fully customizable and will be respected by the migration process.

#### Behavior Options
- `-truncate`: Whether to truncate target tables before migration (default: false)
- `-preserve-case`: Preserve case sensitivity of identifiers using double quotes (default: false)
- `-debug`: Enable debug logging

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

# Preserving case sensitivity
go run cmd/migrate/main.go -source-dsn "sqlserver://sa:StrongPassword@localhost:1433?database=AdventureWorks" \
                          -target-dsn "postgres://postgres:postgres@localhost:5432/adventureworks?sslmode=disable" \
                          -preserve-case

# Skipping tables with filtering options
go run cmd/migrate/main.go -source-dsn "sqlserver://sa:StrongPassword@localhost:1433?database=AdventureWorks" \
                          -target-dsn "postgres://postgres:postgres@localhost:5432/adventureworks?sslmode=disable" \
                          -exclude-tables "log_*,temp_*" \
                          -exclude-empty-tables \
                          -exclude-large-tables 1000000 \
                          -max-table-size 500 \
                          -skip-if-exists
```

## Connection String Formats

### SQL Server

```
sqlserver://username:password@host:port?database=dbname
```

**Important:** The database name must be specified as a query parameter (`?database=dbname`), not as part of the path. The tools will automatically detect and fix incorrectly formatted connection strings where the database is specified in the path (e.g., `sqlserver://user:pass@host:1433/dbname` will be corrected to `sqlserver://user:pass@host:1433?database=dbname`).

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

### Setting Connection Strings

Both tools support multiple ways to specify database connection strings, in the following order of precedence:

1. **Command Line Arguments**:
   ```bash
   # For schema tool
   ./schema -dsn "sqlserver://user:pass@host:1433?database=yourdb"
   
   # For migrate tool
   ./migrate -source-dsn "sqlserver://user:pass@host:1433?database=yourdb" \
             -target-dsn "postgres://postgres:pass@localhost:5432/yourdb?sslmode=disable"
   ```

2. **Environment Variables**:
   ```bash
   # For schema tool
   export DB_DSN="sqlserver://user:pass@host:1433?database=yourdb"
   ./schema
   
   # For migrate tool
   export SOURCE_DB_DSN="sqlserver://user:pass@host:1433?database=yourdb"
   export TARGET_DB_DSN="postgres://postgres:pass@localhost:5432/yourdb?sslmode=disable"
   ./migrate
   ```

3. **Default Values** (if neither command line args nor environment variables are provided)

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

This helps you identify which schemas are available and their size before deciding which ones to include in your migration. The tools use INFORMATION_SCHEMA views to provide accurate table counts for each schema, including system schemas. Schemas are displayed in descending order by table count, so you can easily identify the most data-rich schemas.

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

The generated PostgreSQL schema will:
1. Create the necessary schemas if they don't exist (e.g., `CREATE SCHEMA IF NOT EXISTS dbo;`)
2. Create tables in their respective schemas (e.g., `CREATE TABLE dbo.Users` instead of `CREATE TABLE dbo.Users`)
3. Properly handle tables without schema prefixes by creating them in the public schema
4. Automatically exclude system schemas (like `sys`, `INFORMATION_SCHEMA`, etc.) unless `-include-system-schemas` is specified
5. By default, use unquoted identifiers for schema, table, and column names (which will be lowercase in PostgreSQL)
6. Optionally preserve case sensitivity with the `-preserve-case` flag, which adds double quotes around identifiers

### System Schema Handling

By default, the tools exclude SQL Server system schemas and tables to avoid migration errors:

1. **System schemas** that are excluded:
   - `sys`
   - `INFORMATION_SCHEMA`
   - `db_owner`
   - `db_accessadmin`
   - `db_securityadmin`
   - `db_ddladmin`
   - `db_backupoperator`
   - `db_datareader`
   - `db_datawriter`
   - `db_denydatareader`
   - `db_denydatawriter`

2. **System tables** that are excluded:
   - Any table with a name starting with "sys" (e.g., `systranschemas`, `sysdiagrams`, etc.)

If you need to include system schemas for any reason, use the `-include-system-schemas` flag:

```bash
go run cmd/schema/main.go -dsn "sqlserver://user:pass@host:1433?database=yourdb" -include-system-schemas
```

This ensures that the PostgreSQL database structure properly mirrors the SQL Server schema organization, making it easier to maintain the same access patterns and permissions model.

### Table Filtering Options

The data migration tool provides several options to filter which tables are migrated:

#### Excluding Tables by Name

The `-exclude-tables` flag allows you to specify tables to exclude from migration:

```bash
go run cmd/migrate/main.go -exclude-tables "log_table,temp_data,backup_*"
```

This flag supports wildcards using the `*` character, allowing you to exclude multiple tables with similar names. For example, `log_*` would exclude all tables starting with "log_".

#### Excluding Empty Tables

The `-exclude-empty-tables` flag skips tables that have no rows:

```bash
go run cmd/migrate/main.go -exclude-empty-tables
```

This is useful for skipping tables that are not actively used in the source database.

#### Excluding Large Tables

For large databases, you may want to exclude tables with too many rows or that are too large in size:

```bash
# Skip tables with more than 1 million rows
go run cmd/migrate/main.go -exclude-large-tables 1000000

# Skip tables larger than 500 MB
go run cmd/migrate/main.go -max-table-size 500
```

These options are particularly useful when:
- You need to migrate only the most important data first
- You want to avoid timeouts with very large tables
- You're testing the migration process before running it on all tables

#### Skipping Tables with Existing Data

The `-skip-if-exists` flag skips migration for tables that already have data in the target database:

```bash
go run cmd/migrate/main.go -skip-if-exists
```

This is useful for resuming a previously interrupted migration or for incremental migrations where you only want to migrate tables that haven't been migrated yet.

#### Combining Filtering Options

You can combine multiple filtering options to create a highly customized migration:

```bash
go run cmd/migrate/main.go \
  -exclude-tables "log_*,temp_*,backup_*" \
  -exclude-empty-tables \
  -exclude-large-tables 1000000 \
  -max-table-size 500 \
  -skip-if-exists
```

This combination would:
1. Exclude all tables matching the specified patterns
2. Skip any empty tables
3. Skip tables with more than 1 million rows
4. Skip tables larger than 500 MB
5. Skip tables that already have data in the target database

This level of filtering gives you precise control over which tables are migrated, allowing you to optimize the migration process for your specific needs.

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

### Complete Migration with Case Sensitivity Preserved

If you need to preserve case sensitivity in your PostgreSQL database:

1. Generate the PostgreSQL schema with case sensitivity:
   ```bash
   go run cmd/schema/main.go -dsn "sqlserver://sa:pass@localhost:1433?database=yourdb" -schemas "dbo,sales,hr" -preserve-case
   ```

2. Apply the generated schema to your PostgreSQL database:
   ```bash
   psql -U postgres -d yourdb -f postgres_schema.sql
   ```

3. Migrate the data with case sensitivity:
   ```bash
   go run cmd/migrate/main.go -source-dsn "sqlserver://sa:pass@localhost:1433?database=yourdb" \
                             -target-dsn "postgres://postgres:pass@localhost:5432/yourdb?sslmode=disable" \
                             -schemas "dbo,sales,hr" -preserve-case
   ```

This will ensure that all identifiers (schema names, table names, and column names) maintain their original case from SQL Server.

## Using the Docker Image

You can also use the pre-built Docker image `wang/dbmigrate` to run the migration tools without installing Go or building the project.

### Pulling the Image

```bash
docker pull wang/dbmigrate:latest
```

### Running the Schema Migration Tool

```bash
docker run --rm \
  -e DB_DSN="sqlserver://user:password@host:1433?database=yourdb" \
  -v $(pwd):/output \
  wang/dbmigrate:latest \
  /app/schema -schemas "dbo,sales" > /output/postgres_schema.sql
```

### Running the Data Migration Tool

```bash
docker run --rm \
  -e SOURCE_DB_DSN="sqlserver://user:password@host:1433?database=yourdb" \
  -e TARGET_DB_DSN="postgres://postgres:password@host:5432/yourdb?sslmode=disable" \
  wang/dbmigrate:latest \
  /app/migrate -schemas "dbo,sales"
```

### Using Command-line Arguments Instead of Environment Variables

```bash
docker run --rm \
  wang/dbmigrate:latest \
  /app/schema -dsn "sqlserver://user:password@host:1433?database=yourdb" -schemas "dbo,sales" > postgres_schema.sql
```

### Complete Migration Process with Docker

1. Generate the PostgreSQL schema:
   ```bash
   docker run --rm \
     -v $(pwd):/output \
     wang/dbmigrate:latest \
     /app/schema -dsn "sqlserver://user:password@host:1433?database=yourdb" -schemas "dbo,sales" > postgres_schema.sql
   ```

2. Apply the generated schema to your PostgreSQL database:
   ```bash
   # If PostgreSQL is running locally
   psql -U postgres -d yourdb -f postgres_schema.sql
   
   # Or using Docker
   docker run --rm \
     -v $(pwd):/input \
     postgres:latest \
     psql -h host -U postgres -d yourdb -f /input/postgres_schema.sql
   ```

3. Migrate the data:
   ```bash
   docker run --rm \
     wang/dbmigrate:latest \
     /app/migrate -source-dsn "sqlserver://user:password@host:1433?database=yourdb" \
                 -target-dsn "postgres://postgres:password@host:5432/yourdb?sslmode=disable" \
                 -schemas "dbo,sales"
   ```

### Complete Migration with Case Sensitivity Using Docker

1. Generate the PostgreSQL schema with case sensitivity:
   ```bash
   docker run --rm \
     -v $(pwd):/output \
     wang/dbmigrate:latest \
     /app/schema -dsn "sqlserver://user:password@host:1433?database=yourdb" -schemas "dbo,sales" -preserve-case > postgres_schema.sql
   ```

2. Apply the generated schema to your PostgreSQL database:
   ```bash
   psql -U postgres -d yourdb -f postgres_schema.sql
   ```

3. Migrate the data with case sensitivity:
   ```bash
   docker run --rm \
     wang/dbmigrate:latest \
     /app/migrate -source-dsn "sqlserver://user:password@host:1433?database=yourdb" \
                 -target-dsn "postgres://postgres:password@host:5432/yourdb?sslmode=disable" \
                 -schemas "dbo,sales" -preserve-case
   ```

### Notes

- Replace `user:password@host` with your actual database credentials
- The `-v $(pwd):/output` mounts your current directory to /output in the container for file output
- Use `--network host` if your databases are running on localhost
- For databases in Docker containers, use Docker networking to connect them
