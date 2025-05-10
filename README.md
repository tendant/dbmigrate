# dbmigrate

A tool for migrating SQL Server database schemas to PostgreSQL.

## Description

dbmigrate connects to a SQL Server database, extracts the schema information, and generates a PostgreSQL-compatible schema file. It maps SQL Server data types to their PostgreSQL equivalents and preserves primary key constraints.

## Usage

### Setting the Database Connection

You can specify the database connection string in one of three ways (in order of precedence):

1. **Command Line Flag**:
   ```
   dbmigrate -dsn "sqlserver://sa:pass@localhost:1433?database=yourdb"
   ```

2. **Environment Variable**:
   ```
   export DB_DSN="sqlserver://user:pass@localhost:1433?database=yourdb"
   dbmigrate
   ```

3. **Default Value**:
   If neither the command line flag nor the environment variable is set, the tool will use a default connection string and display a warning.

### Connection String Format

The connection string should be in the following format:
```
sqlserver://username:password@host:port?database=dbname
```

## Output

The tool generates a file named `postgres_schema.sql` containing the PostgreSQL-compatible schema definitions. It also prints the schema to the console.

## Example

```bash
# Using command line flag
dbmigrate -dsn "sqlserver://sa:StrongPassword@localhost:1433?database=AdventureWorks"

# Using environment variable
export DB_DSN="sqlserver://sa:StrongPassword@localhost:1433?database=AdventureWorks"
dbmigrate
```
