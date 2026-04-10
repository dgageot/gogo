---
title: Dotenv
---

# Dotenv

gogo can load environment variables from `.env` files. Variables are injected into all task commands but don't override existing environment variables.

```yaml
dotenv:
  - .env
  - .env.local
```

## File Format

Standard `.env` format with support for comments and quoted values:

```
# Database config
DB_HOST=localhost
DB_PORT=5432
DB_NAME="myapp_dev"
DB_PASSWORD='s3cret'
```

## Resolution Rules

- Files are loaded in order; later files override earlier ones
- Existing environment variables are never overridden
- Missing files are silently skipped
- Paths are relative to the taskfile directory
- `~/` is expanded to the home directory

## With Includes

When using [includes](../includes/), each included taskfile can define its own `dotenv` files. Files are deduplicated by absolute path — the same `.env` file is never loaded twice.
