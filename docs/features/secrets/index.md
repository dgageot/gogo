---
title: Secrets
---

# Secrets

gogo can retrieve secrets from the macOS Keychain or 1Password and inject them as environment variables. Secrets are loaded lazily — only when a task that needs them is executed.

## Keychain Secrets (macOS)

```yaml
secrets:
  API_KEY: keychain://myservice/api-key

deploy:
  secrets: [API_KEY]
  cmd: deploy --api-key $API_KEY
```

The format is `keychain://service/key`. On macOS, gogo uses a Swift helper that authenticates with Touch ID before reading secrets.

### Storing Keychain Secrets

```sh
gogo secret set myservice api-key sk-abc123
```

## 1Password Secrets

```yaml
secrets:
  DB_PASSWORD: 1password://myaccount/vault/item/field

migrate:
  secrets: [DB_PASSWORD]
  cmd: migrate --password $DB_PASSWORD
```

The format is `1password://account/vault/item/field`. The account portion can be a short name (e.g. `myteam`) which is expanded to `myteam.1password.com`.

### Authentication

- If `OP_SERVICE_ACCOUNT_TOKEN` is set, it's used automatically (CI/CD)
- Otherwise, the 1Password desktop app integration is used — make sure the app is running and SDK integration is enabled in Settings > Developer

## How It Works

1. Secrets are defined globally in the `secrets` map with name → reference
2. Each task lists which secrets it needs in its `secrets` field
3. When the task runs, only the requested secrets are resolved
4. Secret values are injected as environment variables
5. Existing environment variables are never overridden
