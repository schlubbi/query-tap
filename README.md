# QueryTap

eBPF-based MySQL query observability — capture queries, latency, and comment metadata without proxies or code changes.

QueryTap attaches to a running `mysqld` process via eBPF uprobes, captures SQL queries with per-query latency measurements, extracts metadata from query comments, and exports structured metrics — all without touching application code or MySQL configuration.

## Features

- **Zero-config capture** — attach to any running mysqld, see queries instantly
- **Per-query latency** — uprobe/uretprobe timing on `dispatch_command`
- **Query comment extraction** — parse `/* key=value */` (marginalia) and `/* controller:users action:show */` (Rails-style) metadata into structured tags
- **MySQL-native digests** — resolves fingerprints to `STATEMENT_DIGEST()` hashes for cross-system correlation with `performance_schema`
- **Query fingerprinting** — normalizes queries (literals → `?`) with cardinality control (LRU eviction)
- **Interactive TUI** — htop-style dashboard with sortable columns, live refresh
- **Streaming output** — text or JSON (NDJSON) to stdout for log pipelines
- **Single static binary** — no runtime dependencies beyond Linux kernel ≥5.8

## Quick Start

```bash
# Build (requires Linux with clang/llvm for BPF compilation)
make generate  # compile BPF C → Go
make build     # build the binary

# Run (requires root or CAP_BPF+CAP_PERFMON)
sudo ./bin/querytap              # TUI mode (default)
sudo ./bin/querytap --stream     # streaming text output
sudo ./bin/querytap --stream --format=json  # NDJSON output
```

## Requirements

- **Linux kernel ≥5.8** (ring buffer support)
- **Root or CAP_BPF + CAP_PERFMON** capabilities
- **mysqld with debug symbols** (`dispatch_command` symbol must be present)
- MySQL 8.0, 8.4 LTS, or Innovation releases (8.1–8.3)

## CLI Flags

```
querytap [flags]

  --mysql-path PATH       Path to mysqld binary (auto-detected if omitted)
  --pid PID               Attach to specific mysqld PID (all instances if omitted)
  --stream                Stream events to stdout instead of TUI
  --format text|json      Output format (default: text)
  --max-fingerprints N    Max unique query fingerprints (default: 10000)
  --max-query-len N       Max query text bytes to capture (default: 4096)
  --ringbuf-size BYTES    BPF ring buffer size (default: 16MB)
  --mysql-dsn DSN         MySQL DSN for STATEMENT_DIGEST resolution
  --filter REGEX          Only capture queries matching regex
  --comment-parser NAME   Comment parser: marginalia (default), rails
  --top N                 Top queries in TUI (default: 20)
  --interval SECONDS      TUI refresh interval (default: 1)
  --verbose               Enable debug logging
  --version               Print version and exit
```

## How It Works

QueryTap attaches eBPF uprobes to MySQL's `dispatch_command` function:

1. **Entry probe** reads the SQL query text from `COM_DATA.com_query.query` and submits it to a ring buffer
2. **Return probe** computes latency (return timestamp − entry timestamp) and submits a latency event
3. **Go userspace** reads the ring buffer, correlates events by thread ID, extracts comments, fingerprints queries, and aggregates metrics

```
mysqld (dispatch_command)
    ↓ uprobe
BPF ring buffer
    ↓
Go: decode → comment parse → fingerprint → aggregate
    ↓
TUI / stream / export
```

## Query Comment Extraction

QueryTap extracts metadata from SQL comments, enabling per-endpoint or per-service query attribution:

```sql
/* app=web, endpoint=/api/users, trace_id=abc123 */ SELECT * FROM users WHERE id = ?
```

Supported formats:
- **Marginalia** (default): `/* key=value, key=value */`
- **Rails**: `/* controller:users action:show */`

## Known Limitations

- **Managed MySQL (RDS, Cloud SQL)** — cannot attach uprobes to opaque managed binaries
- **Stripped binaries** — `dispatch_command` symbol must be present (socket kprobe fallback planned)
- **MySQL 5.x** — different function signature, not supported
- **macOS** — eBPF requires Linux

## Building from Source

```bash
# Prerequisites (Ubuntu/Debian)
sudo apt-get install -y clang llvm libbpf-dev linux-headers-$(uname -r)

# Build
git clone https://github.com/schlubbi/query-tap.git
cd query-tap
make generate
make build
```

## Development

```bash
# Run tests (cross-platform)
make test

# Run integration tests (Linux only, needs Docker)
make test-integration

# Lint
make lint
```

GitHub Codespaces with `--privileged` provides the simplest dev environment.

## License

MIT
