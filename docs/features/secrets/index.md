---
title: Secrets
---

# Secrets

gogo integrates with [1Password CLI](https://developer.1password.com/docs/cli/) to inject secrets as environment variables. When a task's environment contains `op://` references, gogo automatically wraps the command with `op run` to resolve them.

## Usage

```yaml
tasks:
  deploy:
    env:
      DB_PASSWORD: op://vault/item/field
      API_KEY: op://vault/api-credentials/key
    cmd: deploy --password $DB_PASSWORD --api-key $API_KEY
```

The `op://` references use the standard [1Password secret reference](https://developer.1password.com/docs/cli/secret-references/) format.

## How It Works

1. Define `op://` references in a task's `env` map
2. When the task runs, gogo detects the `op://` values
3. The command is wrapped with `op run`, which resolves all references and injects the actual secret values
4. `op` handles authentication automatically, including triggering Touch ID when 1Password is locked

## Requirements

The `op` CLI must be installed and available on the PATH. Install it from https://developer.1password.com/docs/cli/get-started/
