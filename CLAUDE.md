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

- **`main`** — mainline branch; push here to trigger snapshot builds
- **Tags `v*.*.*`** — trigger GitHub Releases with multi-platform binaries via goreleaser (free edition)

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
| `build.yml` | push/PR to main | `go build`, `go test` on Linux, macOS, Windows |
| `lint.yml` | push/PR to main | golangci-lint |
| `snapshot.yml` | push to main | goreleaser snapshot build (no release) |
| `release.yml` | push tag `v*.*.*` | goreleaser release → GitHub Releases |
| `security.yml` | push/PR/nightly | CodeQL, Grype, govulncheck |

## Development notes

- CGO is disabled (`CGO_ENABLED=0`) — pure Go binary, runs everywhere
- `GOEXPERIMENT=greenteagc` is set for the GC experiment
- Log messages must start with a capital letter (enforced by `scripts/check_log_capitalization.sh`)
- Config lives in `~/.config/crush/` (XDG), workspace data in `.crush/` inside the project dir
