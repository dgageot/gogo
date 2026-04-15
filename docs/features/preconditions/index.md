---
title: Preconditions
---

# Preconditions

Tasks can define preconditions — shell commands that must succeed before the task runs. If any precondition fails, the task stops with an error.

```yaml
tasks:
  deploy:
    preconditions:
      - sh: test -n "$DOCKER_HUB_USER"
        msg: DOCKER_HUB_USER is not set. It is needed to avoid Docker Hub rate limits.
      - sh: test -n "$DOCKER_HUB_PASSWORD"
        msg: DOCKER_HUB_PASSWORD is not set. It is needed to avoid Docker Hub rate limits.
    cmd: docker push myimage
```

## String Shorthand

When you don't need a custom message, use a plain string:

```yaml
tasks:
  deploy:
    preconditions:
      - test -n "$DEPLOY_TOKEN"
      - test -f config.yaml
    cmd: deploy.sh
```

If the precondition fails, gogo prints a default message showing the failed command:

```
task "deploy": precondition failed: test -n "$DEPLOY_TOKEN"
```

## Custom Error Messages

Use the map form to provide a clear, human-readable message:

```yaml
tasks:
  deploy:
    preconditions:
      - sh: test -n "$DEPLOY_TOKEN"
        msg: DEPLOY_TOKEN is not set. Get one from the admin dashboard.
    cmd: deploy.sh
```

```
task "deploy": DEPLOY_TOKEN is not set. Get one from the admin dashboard.
```

## Evaluation Order

Preconditions are checked after dependencies and required variables, but before any commands run.
