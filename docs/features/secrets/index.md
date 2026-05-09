---
title: Secrets
---

# Secrets

gogo integrates with [1Password CLI](https://developer.1password.com/docs/cli/) to inject secrets as environment variables. There are two ways to declare them: a centralised top-level `secrets:` block (recommended for projects with several secrets or several tasks) and inline `op://` URIs in a task's `env:` (the original form, still supported).

Both forms feed into the same code path: any `op://` reference in the resolved environment causes the command to be wrapped with `op run`, which performs the actual lookup at exec time. Authentication (Touch ID, biometric, etc.) flows through `op` exactly as it does today.

## Top-level `secrets:` block

Declare each secret once at the top of the file, then reference it by name from the tasks that need it:

```yaml
secrets:
  OPENAI_API_KEY: op://Team AI Agent/docker-ai/OPENAI_API_KEY
  ANTHROPIC_KEY:  op://Team AI Agent/docker-ai/ANTHROPIC_API_KEY

tasks:
  test:
    secrets: [OPENAI_API_KEY]
    cmd: poetry run pytest

  dev:
    secrets: [OPENAI_API_KEY, ANTHROPIC_KEY]
    cmd: docker compose up --watch
```

Three reasons this is preferred over inline URIs once you have more than one or two secrets:

1. **Single source of truth.** Rotating a vault path is a one-line edit instead of a grep across N tasks.
2. **Per-task allow-list.** A task only sees the secrets it explicitly opts into.
3. **Forward-compatible.** Adding a new backend (Keychain, AWS, …) later is purely additive.

The map key (`OPENAI_API_KEY`) becomes the env-var name injected into the task. The URI follows the standard [1Password secret reference](https://developer.1password.com/docs/cli/secret-references/) format.

## Inline `op://` (still supported)

The original integration still works. If you have any `op://` URI anywhere in the resolved env (from `task.env`, dotenv, etc.), the command is wrapped with `op run` automatically — no `secrets:` block required:

```yaml
tasks:
  deploy:
    env:
      DB_PASSWORD: op://vault/item/field
    cmd: deploy --password $DB_PASSWORD
```

You can mix and match: a `secrets:` block for the values you want centralised, plus inline `op://` for one-off references.

## Precedence

A task's `secrets:` block sits at the **top** of the environment-composition stack — it overrides task `env:`, task `vars:`, dotenv, inherited parent env, and the process environment. So a task that declares both `env: { OPENAI_API_KEY: DUMMY }` (a placeholder for local dev) and `secrets: [OPENAI_API_KEY]` resolves to the secret at run time. Declaring a secret is a strong signal that the value should come from the backend.

The full layered order is documented once on the [Variables](../variables/#2-final-task-environment-what-the-command-actually-sees) page; everything in this guide composes with that table.

## Validation

A `secrets:` reference that names something not declared at the top level fails fast at load time:

```
task "test" references unknown secret "TYPO"; declare it under top-level `secrets:`
```

So does an unsupported URI scheme:

```
secret "X" has unknown backend in "vault://somewhere/else" (supported: op://)
```

## Requirements

The `op` CLI must be installed and available on PATH. Install it from <https://developer.1password.com/docs/cli/get-started/>.
