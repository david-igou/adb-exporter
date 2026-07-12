# adb-exporter

[![CI](https://github.com/david-igou/adb-exporter/actions/workflows/ci.yml/badge.svg)](https://github.com/david-igou/adb-exporter/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/david-igou/adb-exporter)](https://goreportcard.com/report/github.com/david-igou/adb-exporter)
[![Go Reference](https://pkg.go.dev/badge/github.com/david-igou/adb-exporter.svg)](https://pkg.go.dev/github.com/david-igou/adb-exporter)

A Prometheus exporter for Android devices reachable over `adb`. It shells out
to the `adb` CLI, runs a fixed set of shell commands against the device, and
parses their output into Prometheus metrics — no agent needed on the device.

Built and verified against an **Nvidia Shield TV Pro** (Android 11, `arm`,
toybox userland), but it should work with anything `adb shell` can reach.

## Features

- **Memory, load, uptime, per-process RSS/CPU, thermals, network, filesystem,
  media sessions, and foreground app** — ~120 series from one scrape.
- **No device-side agent.** Just an authorized `adb connect`.
- **Scrape-driven.** Collection happens per `/metrics` request via a custom
  `prometheus.Collector`; there is no background polling loop.
- **Never crashes, never hangs.** Per-command and whole-scrape timeouts; an
  unreachable device serves `adb_up 0` plus scrape-meta metrics.
- **Single static binary.** Only runtime dependency is the `adb` binary itself.

## Quick start

```sh
# Build
make build

# The target device must already be authorized:
adb connect 10.10.9.22:5555   # should succeed without a prompt on the device

# Run
./adb-exporter -adb.address 10.10.9.22:5555

# Metrics
curl http://localhost:9836/metrics
```

### Prometheus scrape config

```yaml
scrape_configs:
  - job_name: adb
    static_configs:
      - targets:
          - adb-exporter-host:9836
```

## Installation

Requires Go 1.26+ to build, and an `adb` binary on `PATH` at runtime (or point
`-adb.path` at one).

```sh
# From a checkout
make build

# Or straight to GOBIN
go install github.com/david-igou/adb-exporter@latest

# Cross-compile for linux/amd64 + linux/arm64 into dist/
make crossbuild
```

## Configuration

All configuration is via flags:

| Flag | Default | Meaning |
|------|---------|---------|
| `-web.listen-address` | `:9836` | HTTP listen address for `/metrics`. |
| `-adb.address` | `10.10.9.22:5555` | `host:port` passed to `adb -s`. |
| `-adb.path` | `adb` | Path to the adb binary. |
| `-adb.timeout` | `5s` | Per-command context timeout. Overall scrape cap = 8×. |
| `-collect.top-processes` | `15` | N: emit the top-N processes by RSS. |
| `-collect.process-include` | `""` | Comma-separated process names always emitted regardless of rank (allowlist). |

## Metrics

All metrics are prefixed `adb_`. Memory and filesystem values are converted from
kB / 1K-blocks to **bytes** (×1024). `%CPU` and `Use%` are exposed as **ratios
0..1** (÷100), per Prometheus base-unit convention.

| Metric | Type | Labels | Source command | Notes |
|--------|------|--------|----------------|-------|
| `adb_up` | gauge | — | `adb get-state` | 1 if device reachable, else 0. |
| `adb_exporter_build_info` | gauge | `version`, `revision`, `goversion` | (build) | Constant 1. |
| `adb_memory_total_bytes` | gauge | — | `cat /proc/meminfo` | MemTotal ×1024. |
| `adb_memory_free_bytes` | gauge | — | `cat /proc/meminfo` | MemFree ×1024. |
| `adb_memory_available_bytes` | gauge | — | `cat /proc/meminfo` | MemAvailable ×1024. |
| `adb_memory_buffers_bytes` | gauge | — | `cat /proc/meminfo` | Buffers ×1024. |
| `adb_memory_cached_bytes` | gauge | — | `cat /proc/meminfo` | Cached ×1024. |
| `adb_memory_swap_total_bytes` | gauge | — | `cat /proc/meminfo` | SwapTotal ×1024. |
| `adb_memory_swap_free_bytes` | gauge | — | `cat /proc/meminfo` | SwapFree ×1024. |
| `adb_load1` / `adb_load5` / `adb_load15` | gauge | — | `cat /proc/loadavg` | 1/5/15-minute loads. |
| `adb_uptime_seconds` | gauge | — | `cat /proc/uptime` | Field 1. |
| `adb_process_memory_rss_bytes` | gauge | `process` | `ps -A -o PID,RSS,%CPU,NAME` | RSS kB ×1024, top-N + allowlist. |
| `adb_process_cpu_ratio` | gauge | `process` | `ps -A -o PID,RSS,%CPU,NAME` | %CPU ÷100 (toybox lifetime average). |
| `adb_thermal_temperature_celsius` | gauge | `name`, `type` | `dumpsys thermalservice` | HAL block, falls back to cached. |
| `adb_network_bytes_total` | counter | `interface`, `direction` | `cat /proc/net/dev` | rx/tx. |
| `adb_network_packets_total` | counter | `interface`, `direction` | `cat /proc/net/dev` | rx/tx. |
| `adb_network_errs_total` | counter | `interface`, `direction` | `cat /proc/net/dev` | rx/tx. |
| `adb_network_drop_total` | counter | `interface`, `direction` | `cat /proc/net/dev` | rx/tx. |
| `adb_filesystem_size_bytes` | gauge | `mountpoint` | `df /data /cache` | 1K-blocks ×1024. |
| `adb_filesystem_used_bytes` | gauge | `mountpoint` | `df /data /cache` | 1K-blocks ×1024. |
| `adb_filesystem_avail_bytes` | gauge | `mountpoint` | `df /data /cache` | 1K-blocks ×1024. |
| `adb_media_session_count` | gauge | — | `dumpsys media_session` | Registered sessions. |
| `adb_media_playback_state` | gauge | `package` | `dumpsys media_session` | Only for actively-playing sessions. |
| `adb_foreground_app_info` | gauge | `package`, `activity` | `dumpsys activity activities \| grep ResumedActivity` | Constant 1 info metric. |
| `adb_scrape_duration_seconds` | gauge | — | (internal) | Wall time of the scrape. |
| `adb_scrape_collector_success` | gauge | `collector` | (internal) | 1/0 per sub-collector, always emitted. |
| `adb_scrape_errors_total` | counter | — | (internal) | Lifetime sub-collector + reconnect failures. |

### Label value references

- **`type`** on `adb_thermal_temperature_celsius` maps Android `Temperature.Type`:
  `CPU`, `GPU`, `BATTERY`, `SKIN`, `USB_PORT`, `POWER_AMPLIFIER`, `BCL_VOLTAGE`,
  `BCL_CURRENT`, `BCL_PERCENTAGE`, `NPU`; unrecognized types are `unknown`.
- **`adb_media_playback_state`** value is the Android `PlaybackState` int:
  `0=NONE, 1=STOPPED, 2=PAUSED, 3=PLAYING, 4=FAST_FORWARDING, 5=REWINDING,
  6=BUFFERING, 7=ERROR, 8=CONNECTING, 9=SKIP_PREV, 10=SKIP_NEXT,
  11=SKIP_QUEUE_ITEM`. Idle sessions (`state=null`) emit no series.

## Design

- **All adb access is serialized.** Concurrent `adb shell` invocations against a
  single device intermittently fail with `request send failed: Permission
  denied`, so every adb command runs through one mutex-guarded worker.
  Collectors run sequentially, never in parallel.
- **CLI, not a protocol library.** It calls the real `adb` binary via
  `os/exec`; it does not reimplement the adb wire protocol.
- **One sub-collector failing never aborts the others.** Each failure sets
  `adb_scrape_collector_success{collector}=0`, increments
  `adb_scrape_errors_total`, and is logged. When the device is down every
  device collector reports `0` and only `adb_up`, `adb_exporter_build_info`,
  `adb_scrape_duration_seconds`, `adb_scrape_collector_success` (all 0), and
  `adb_scrape_errors_total` are emitted.

### Notes on the reference device

- toybox `df` rejects `-B1`; the exporter parses default 1K-block output. `df
  /data` reports the mount as `/data/user/0`, used verbatim as the `mountpoint`.
- toybox `ps` `%CPU` is a lifetime average, not an instantaneous sample.
- Media playback state only appears while media is active; an idle session with
  `state=null` is a successful scrape that emits only `adb_media_session_count`.

## Development

```sh
make help    # list all targets
make all     # vet + test + build
make test    # unit tests — no device needed
make race    # tests with the race detector
make cover   # tests with coverage summary
make lint    # vet + gofmt check (+ golangci-lint if installed)
make fmt     # gofmt the tree
```

The unit tests need no device: every parser is table-driven against real
sample outputs captured from the reference device.

### Project layout

```
main.go               flag parsing, HTTP server, wiring
internal/adb/         serialized adb CLI client (the mutex-guarded worker)
internal/collector/   one file per sub-collector + its parser tests
```

### CI

GitHub Actions ([`ci.yml`](.github/workflows/ci.yml)) runs on every push and
PR: gofmt/vet/`go mod tidy` checks and golangci-lint, unit tests with the race
detector and coverage, and cross-compile builds for linux/amd64 and
linux/arm64.
