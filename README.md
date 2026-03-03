# QueryTap

A zero-instrumentation MySQL query observer that uses eBPF uprobes to capture
queries directly from a running `mysqld` process — no slow-query log, no
`performance_schema`, no proxies.

QueryTap attaches to the `dispatch_command` function inside the MySQL binary,
captures every SQL statement in real time, normalizes it into a fingerprint,
and aggregates per-fingerprint latency metrics (count, p50, p99, max). Output
is available as a live TUI dashboard or a streaming JSON/text feed.

## Features

- **Zero overhead** — eBPF uprobes add negligible latency to query execution
- **No MySQL config changes** — works on any unmodified `mysqld` binary with debug symbols (DWARF)
- **Fingerprinting** — normalizes queries so `SELECT * FROM t WHERE id=1` and `id=2` share a fingerprint
- **Comment parsing** — extracts structured metadata from SQL comments (e.g., `marginalia`, `sqlcommenter`)
- **Live TUI** — interactive top-like dashboard sorted by query count, latency, or error rate
- **Streaming mode** — pipe JSON or text output to files, `jq`, or downstream systems
- **Telemetry export** — optional OTLP and DogStatsD metric export
- **STATEMENT_DIGEST** — optional MySQL connection to resolve canonical digests

## Requirements

- Linux kernel ≥ 5.8 (BPF ring buffer support)
- Root privileges or `CAP_BPF` + `CAP_PERFMON` capabilities
- `mysqld` binary with DWARF debug information (debug symbols)
- Go 1.23+ (build-time only)
- clang/llvm 14+ (build-time only, for BPF compilation)
- libbpf-dev (build-time only)

## Building

```bash
# Generate BPF code and build the binary
make generate
make build

# Run tests
make test

# Lint
make lint
```

## Usage

```bash
# Auto-detect mysqld and start the TUI dashboard
sudo ./bin/querytap

# Stream JSON output for a specific mysqld binary
sudo ./bin/querytap --mysql-path /usr/sbin/mysqld --stream --format json

# Attach to a specific PID
sudo ./bin/querytap --pid 12345

# Export metrics via OTLP
sudo ./bin/querytap --otlp-endpoint localhost:4317

# Filter queries matching a pattern
sudo ./bin/querytap --filter "SELECT.*FROM users"
```

## License

MIT — see [LICENSE](LICENSE).
