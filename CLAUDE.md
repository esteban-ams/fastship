# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

DeployDeck is a lightweight Go webhook server that automates Docker Compose deployments. It supports two modes: **pull** (deploy pre-built images) and **build** (clone repo + build on server). It listens for webhooks from CI/CD pipelines (GitHub Actions, GitLab CI) or direct GitHub/GitLab push events and orchestrates container deployments with health checking and rollback support.

## Build & Development Commands

```bash
make build          # Build binary for current platform
make build-linux    # Cross-compile for Linux amd64
make run            # Run with go run (expects config.yaml)
make test           # Run all tests (go test -v ./...)
make deps           # Download and tidy dependencies
make install        # Install to GOPATH/bin
make clean          # Remove build artifacts
```

Run with flags: `./deploydeck --config config.yaml --port 8000 --version`

No linter is configured.

## Architecture

**Request flow**: Webhook HTTP request → Auth verification → Payload parsing (build mode) → Deployment engine → Git clone (build) or Docker pull → Docker Compose up → Health check → Success/Rollback

### Key packages under `internal/`:

- **config/** — YAML config parsing with environment variable overrides (`DEPLOYDECK_PORT`, `DEPLOYDECK_HOST`, `DEPLOYDECK_WEBHOOK_SECRET`, `DEPLOYDECK_LOG_LEVEL`, `DEPLOYDECK_CLONE_TOKEN`). Token resolution from files (Docker Secrets pattern). Config precedence: CLI flags > env vars > config.yaml > defaults.
- **webhook/** — Echo HTTP handlers, authentication (3 methods: GitHub HMAC, GitLab token, DeployDeck secret), and push event payload parsing (GitHub/GitLab). Branch filtering for build mode.
- **deploy/** — Deployment orchestration with per-service mutex. 7-step pipeline: save image → mode-specific (clone+build or pull) → compose up → health check → success → cleanup rollback tags → auto-prune. State machine: `pending → running → success/failed/rolled_back`. Rollback via image tagging. Per-service timeouts (default 5m pull, 10m build). Storage is pluggable: SQLite when `storage.db_path` is set, in-memory otherwise.
- **docker/** — Wraps `docker compose` CLI via `os/exec`. Methods: ComposePull, ComposeBuild, ComposeUp, GetCurrentImage, GetContainerName, TagImage, RemoveImage, ListImagesByFilter, BuilderPrune.
- **git/** — Git clone with shallow depth and automatic token injection per provider (GitHub: x-access-token, GitLab: oauth2).

### Entry point

`cmd/deploydeck/main.go` — Sets up Echo router, registers routes, and starts the HTTP server.

## API Endpoints

- `POST /api/deploy/:service` — Trigger deployment (requires auth header). Build mode parses push webhook payload; pull mode accepts `{"image": "...", "tag": "..."}`.
- `POST /api/rollback/:service` — Manual rollback (requires auth)
- `GET /api/deployments` — List all deployments (SQLite-backed when `storage.db_path` is set, otherwise in-memory)
- `GET /api/deployments/:id/logs` — Deployment log lines, polled by `deploydeck logs -f`
- `GET /api/health` — Health check with version and uptime
- `GET /dashboard/` — Embedded web dashboard (enable via `dashboard.enabled: true`)

## CLI Client Subcommands

`cmd/deploydeck/` also ships a thin HTTP client so operators don't need to hand-write `curl`. All subcommands read `--server`/`-s` (default `http://localhost:9000` or `$DEPLOYDECK_SERVER`) and `--secret` (default `$DEPLOYDECK_SECRET`):

- `deploydeck init` — interactive wizard that scaffolds `config.yaml` (generates the webhook secret) and prints a ready-to-paste CI workflow snippet
- `deploydeck doctor` — checks Docker CLI/daemon, Compose, Git, config file validity, and that each service's compose file exists
- `deploydeck deploy <service> [--image --tag]` — `POST /api/deploy/:service`
- `deploydeck rollback <service>` — `POST /api/rollback/:service`
- `deploydeck status` — latest deployment per service (`GET /api/deployments`, deduped)
- `deploydeck logs <service> [-f]` — deployment log lines, `-f` polls until terminal state
- `deploydeck config` — prints the services configured in the local `config.yaml`
- `deploydeck version` — prints the build version

## Configuration

Copy `config.example.yaml` to `config.yaml`. Services support two modes (`pull` and `build`) with compose file path, working directory, health check settings, rollback options, timeouts, branch filtering, and token security.

## CI/CD

GitHub Actions (`.github/workflows/build.yml`): tests on all PRs/pushes, builds multi-platform binaries on version tags, publishes Docker image to `ghcr.io/esteban-ams/deploydeck` on main push and tags.
