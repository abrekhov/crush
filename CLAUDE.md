# Crush — Project Context for Claude

## What this is

Fork of [charmbracelet/crush](https://github.com/charmbracelet/crush) — a terminal-first AI coding assistant written in Go.
Module path: `github.com/abrekhov/crush`

## Key architecture

- **BubbleTea TUI** (`internal/ui/model/ui.go`) — full-featured terminal UI
- **HTTP server** (`internal/server/`) — listens on Unix socket (`/tmp/crush-{uid}.sock`) or TCP
- **Workspace abstraction** (`internal/workspace/`) — `AppWorkspace` (in-process) or `ClientWorkspace` (HTTP client)
- **Agent loop** (`internal/agent/`) — coordinates LLM calls via the `fantasy` library
- **Database** — SQLite via `go-sqlite3`, queries in `internal/db/`

## Server mode (our addition)

Added in `internal/cmd/server.go` and `internal/cmd/root.go`:

| Command | Behaviour |
|---------|-----------|
| `crush server` | Starts HTTP server. If interactive terminal detected, also opens a TUI. Quit TUI → server keeps running headlessly. Ctrl+C → server stops. |
| `crush attach` | Connects to a running server and opens a TUI. Fails fast if no server is running. |
| `crush` | In-process mode (no server). Set `CRUSH_CLIENT_SERVER=1` to force client/server mode. |

Key helpers in `root.go`:
- `runTUI(cmd, ws, sessionID, continueLast)` — shared BubbleTea bootstrap
- `connectToServerOnly(cmd, hostURL)` — connect without auto-starting a server
- `setupAttachWorkspace(cmd, hostURL)` — wraps above in a `ClientWorkspace`

## Branching & releases

Two long-lived branches:

- **`main`** — a read-only mirror of `upstream/main` (charmbracelet/crush). Never commit here.
  It exists purely so upstream history stays clean and resettable.
- **`abrekhov/main`** — the working branch. All fork features land here; this is the
  default branch on GitHub and the only branch CI builds.
- **Tags `v*.*.*`** — trigger GitHub Releases with multi-platform binaries via goreleaser (free edition)

Single developer — commit and push directly to `abrekhov/main`. No PRs, no feature branches needed.

To absorb upstream changes:
```bash
git fetch upstream
git checkout main && git merge --ff-only upstream/main   # mirror, never conflicts
git checkout abrekhov/main
git tag pre-upstream-$(date +%Y%m%d)                     # bisect anchor
git merge main                                           # conflicts resolved here only
```

If a merge goes wrong, `git merge --abort` and retry — `main` is always pristine.
Expect `internal/config/config.go` to conflict every time: upstream deletes the
`providers.anthropic` block to force onboarding, and this fork restores it.

To cut a release:
```bash
git tag v0.1.0
git push origin v0.1.0
```

Goreleaser builds: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`, `windows/arm64`.
Archive naming: `crush_{version}_Linux_x86_64.tar.gz` etc.

## Running tests

```bash
go test ./...                    # full suite
go test ./internal/cmd/...       # cmd package (includes server_test.go)
go test ./internal/agent/...     # agent loop (uses VCR recordings)
```

## VPS deployment (linux/amd64)

### Install

```bash
# Replace 0.1.0 with the actual version number (no "v" in the archive filename)
VERSION=0.1.0
curl -L "https://github.com/abrekhov/crush/releases/download/v${VERSION}/crush_${VERSION}_Linux_x86_64.tar.gz" \
  | tar -xz --strip-components=1
sudo install -m755 crush /usr/local/bin/crush
crush --version
```

### Run server (headless, persists across SSH disconnects)

```bash
# Option A — nohup (simplest)
nohup crush server > ~/crush-server.log 2>&1 &
echo $! > ~/crush-server.pid

# Option B — systemd (recommended for production)
sudo tee /etc/systemd/system/crush.service <<'EOF'
[Unit]
Description=Crush AI Server
After=network.target

[Service]
Type=simple
User=$USER
ExecStart=/usr/local/bin/crush server
Restart=on-failure
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF
sudo systemctl daemon-reload
sudo systemctl enable --now crush
```

### Attach a TUI to the running server

```bash
# From any SSH session on the same VPS:
crush attach

# Continue a specific session:
crush attach --session <session-id>

# Continue the most recent session:
crush attach --continue

# Stop the server gracefully (nohup variant):
kill $(cat ~/crush-server.pid)

# Stop systemd service:
sudo systemctl stop crush
```

### Default socket path

```
/tmp/crush-$(id -u).sock
```

Override with `--host unix:///custom/path.sock` on both `crush server` and `crush attach`.

## CI/CD workflows

| Workflow | Trigger | What it does |
|----------|---------|--------------|
| `build.yml` | push/PR to `abrekhov/main` | `go build`, `go test` on Linux, macOS, Windows |
| `lint.yml` | push/PR to `abrekhov/main` | golangci-lint |
| `snapshot.yml` | push to `abrekhov/main` | goreleaser snapshot build (no release) |
| `release.yml` | push tag `v*.*.*` | goreleaser release → GitHub Releases |
| `security.yml` | push/PR/nightly | CodeQL, Grype, govulncheck |
| `schema-update.yml` | push to `abrekhov/main` touching config | regenerates `schema.json`, commits back |

Workflows are pinned to `abrekhov/main` so the upstream mirror never triggers builds.

Three upstream workflows are **disabled at the repository level** (not in the files, so
upstream merges stay conflict-free): `nightly`, `CLA Assistant`, and `labeler`. They depend
on Charm organization secrets this fork does not have. Re-enable with `gh workflow enable <name>`.

Lint runs `gofumpt`, which sorts the module's own imports differently from upstream's because
of the fork rename. Run `gofumpt -w .` before pushing or lint will fail.

## Authentication (providers)

### claude.ai Pro/Max subscription (OAuth)

```bash
# On the VPS (or any machine with crush installed):
crush login anthropic
# Prints a claude.ai authorization URL → open in browser → authorize →
# paste the 'code' from the redirect URL back into the prompt.
```

OAuth details (implementation in `internal/oauth/anthropic/pkce.go`):
- **Client ID**: `9d1c250a-e61b-44d9-88ed-5944d1962f5e`
- **Auth URL**: `https://claude.ai/oauth/authorize`
- **Token URL**: `https://console.anthropic.com/v1/oauth/token`
- **Redirect URI**: `https://console.anthropic.com/oauth/code/callback`
- **Scopes**: `org:create_api_key user:profile user:inference`
- **PKCE**: S256 (no client secret needed)

Tokens are stored in `~/.local/share/crush/crush.json` (the data directory, *not*
`~/.config/crush/`) under `providers.anthropic.oauth`. Run `crush dirs` to confirm
the paths on a given machine. The access token auto-refreshes via
`providers.anthropic.oauth.refresh_token`.

Paste the whole `code` value from the redirect URL — it looks like `<code>#<state>`,
and `ExchangeCode` splits the fragment off itself.

**What makes subscription inference work** (`ProviderConfig.SetupAnthropicOAuth` in
`internal/config/config.go`) — all three are required, drop one and the API 401s:

1. **`Authorization: Bearer <token>`, not `x-api-key`.** Subscription tokens are
   rejected as an API key. Setup rewrites `APIKey` to `"Bearer " + token`;
   `buildAnthropicProvider` (`internal/agent/coordinator.go`) keys off that exact
   prefix to send an `Authorization` header and clear `ANTHROPIC_API_KEY`.
2. **`anthropic-beta: oauth-2025-04-20`** — added via `ExtraHeaders`.
3. **Claude Code identity as the first system block.** The subscription endpoint is
   scoped to Claude Code and rejects requests that do not lead with
   `You are Claude Code, Anthropic's official CLI for Claude.` Setup puts it at the
   front of `SystemPromptPrefix`, which the agent prepends as a leading system
   message; a user-configured prefix is preserved after it.

`SetupAnthropicOAuth` is idempotent — config reloads re-run it over an
already-prepared provider. It runs on login, on config load, and after a 401
refresh, so the three properties survive a token rotation.

All three are applied **in memory at load time and never written to disk**. On disk
you will see the raw token with no `Bearer ` prefix, no `extra_headers`, and no
system prompt prefix — that is correct, not a broken login. Judge the login by
whether `providers.anthropic.oauth.refresh_token` exists, not by those three fields.

Note: upstream **removed** Claude Code subscription support (it deletes the
`providers.anthropic` config to force onboarding). This fork restores it, so expect
that hunk to conflict on every upstream merge.

### GitHub Copilot

```bash
crush login copilot
```

### Hyper

```bash
crush login hyper   # or just: crush login
```

## Development notes

- CGO is disabled (`CGO_ENABLED=0`) — pure Go binary, runs everywhere
- `GOEXPERIMENT=greenteagc` is set for the GC experiment
- Log messages must start with a capital letter (enforced by `scripts/check_log_capitalization.sh`)
- Config lives in `~/.config/crush/` (XDG), credentials and provider cache in
  `~/.local/share/crush/`, workspace data in `.crush/` inside the project dir.
  `crush dirs` prints all of them.
