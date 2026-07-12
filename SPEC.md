# adb-exporter — Specification

A Prometheus exporter that scrapes an Android TV device over `adb` by shelling out to
the `adb` CLI (`os/exec`) and parsing the output of a fixed set of shell commands.

- Target reference device: Nvidia Shield TV Pro, Android 11, `arm`, toybox userland,
  reachable at `10.10.9.22:5555`.
- All sample output in this document was captured from that live device and is the
  authoritative parser contract. Android 11 uses **toybox** `ps`/`df`/`top`, which differ
  from GNU coreutils; parsers must match the toybox format shown here, not GNU.

## 0. Hard constraints (non-negotiable)

- **Serialize all adb access.** Concurrent `adb shell` invocations against this device
  intermittently fail with `request send failed: Permission denied`. The exporter MUST run
  every adb command through a single worker guarded by a `sync.Mutex` (or a 1-deep worker
  goroutine). Collectors run **sequentially**, never in parallel goroutines.
- **Shell out to the `adb` CLI** via `os/exec`. Do NOT use a pure-Go adb protocol library.
- **Collect per-scrape** via a custom `prometheus.Collector` (implement `Collect`/`Describe`).
  No background polling loop.
- **Never crash, never hang.** Every command runs under a per-command context timeout, and
  the whole scrape runs under an overall context timeout. If the device is unreachable the
  exporter serves `adb_up 0` plus scrape-meta metrics and **omits** all device metrics.

## 1. Runtime model

```
promhttp GET /metrics
  -> Collector.Collect(ch)
       -> acquire adbClient mutex (whole scrape holds ordering via sequential calls)
       -> ensureConnected(): if `adb get-state` != "device", try `adb connect <addr>`
       -> if still not "device": emit adb_up=0 + meta, return
       -> emit adb_up=1
       -> for each collector in fixed order (sequential):
            run its source command(s) under per-command timeout
            parse; on success emit metrics + collector_success=1
            on error: log, collector_success=0, increment adb_scrape_errors_total, continue
       -> emit adb_scrape_duration_seconds
```

Overall scrape budget defaults to `-adb.timeout` (per-command) with an overall cap of
`8 * per-command` (see flags). Each `runADB` call is `exec.CommandContext` with the
per-command timeout; a timed-out command counts as a collector failure, not a crash.

## 2. adb invocation contract

Base command: `<adb.path> -s <adb.address> <args...>`.

- Connect check: `adb -s <addr> get-state` → prints `device` when healthy (verified).
- Reconnect: `adb connect <addr>` → `already connected...` or `connected to ...`.
- Shell reads: `adb -s <addr> shell <cmd>`. Commands are single tokens or quoted pipelines;
  keep them minimal and prefer reading `/proc` files with `cat`.
- stdout is captured and parsed; non-zero exit or empty stdout ⇒ collector error.
- Line endings: adb shell emits `\n` here (no `\r`); parsers should still `TrimRight` `\r`.

## 3. Flags

| Flag | Default | Meaning |
|------|---------|---------|
| `-web.listen-address` | `:9836` | HTTP listen address for `/metrics`. |
| `-adb.address` | `10.10.9.22:5555` | `host:port` passed to `adb -s`. |
| `-adb.path` | `adb` | Path to the adb binary. |
| `-adb.timeout` | `5s` | Per-command context timeout. Overall scrape cap = 8×. |
| `-collect.top-processes` | `15` | N: emit the top-N processes by RSS. |
| `-collect.process-include` | `""` | Comma-separated package/process names always emitted regardless of rank (allowlist). |

Standard `promhttp` handler at `/metrics`; a minimal `/` landing page is optional.

## 4. Metric naming convention

- Prefix every metric `adb_`.
- Base SI units only: bytes (`_bytes`), seconds (`_seconds`), ratios `0..1` where noted.
- Counters end in `_total`. Gauges use no reserved suffix beyond the unit.
- `%CPU` and `Use%` are exposed as **ratios 0..1** (divide the percentage by 100), per
  Prometheus best practice — e.g. ps `3.6` → `0.036`.
- Memory/df values arrive in **kB / 1K-blocks** and MUST be multiplied by **1024** to bytes.

## 5. Package layout

```
/workspace/adb-exporter/
  go.mod                       module github.com/david-igou/adb-exporter
  main.go                      flags, adbClient, Collector wiring, http server
  internal/adb/client.go       serialized exec wrapper: RunShell, RunRaw, State, Connect
  internal/collector/collector.go   top-level Collector, ordering, meta metrics, adb_up
  internal/collector/meminfo.go
  internal/collector/loadavg.go
  internal/collector/uptime.go
  internal/collector/process.go
  internal/collector/thermal.go
  internal/collector/netdev.go
  internal/collector/storage.go
  internal/collector/mediasession.go
  internal/collector/foreground.go
```

Each collector file exposes: `Name() string`, its `*prometheus.Desc` vars, and
`Collect(ctx, adb, ch) error`. The top-level Collector owns the fixed ordering and the
meta/`adb_up` metrics. Sub-collectors never touch `adb_up` or the mutex directly — they
receive the already-serialized `adb` client.

## 6. Metric families & parser contracts

### 6.0 adb_up + build info

```
# HELP adb_up 1 if the device is reachable (adb state == device), else 0.
# TYPE adb_up gauge
adb_up 1
# HELP adb_exporter_build_info Build metadata.
# TYPE adb_exporter_build_info gauge
adb_exporter_build_info{version="...",revision="...",goversion="..."} 1
```

`adb_up` derives from `adb get-state` (after an optional reconnect). `build_info` is a
constant `1` gauge set at startup from `-ldflags` values (fallback `"dev"`).

### 6.1 Memory — `adb shell cat /proc/meminfo`

Real output (excerpt):
```
MemTotal:        3016708 kB
MemFree:          134236 kB
MemAvailable:    1497592 kB
Buffers:           22760 kB
Cached:          1294048 kB
SwapTotal:        524284 kB
SwapFree:         524184 kB
```
Parse `^(\w+):\s+(\d+) kB`. Multiply value ×1024 → bytes. Emit only the seven keys below;
ignore all others. Missing key ⇒ skip that metric (not an error).

| Metric | meminfo key |
|--------|-------------|
| `adb_memory_total_bytes` | MemTotal |
| `adb_memory_free_bytes` | MemFree |
| `adb_memory_available_bytes` | MemAvailable |
| `adb_memory_buffers_bytes` | Buffers |
| `adb_memory_cached_bytes` | Cached |
| `adb_memory_swap_total_bytes` | SwapTotal |
| `adb_memory_swap_free_bytes` | SwapFree |

All gauges, no labels.

### 6.2 Load — `adb shell cat /proc/loadavg`

Real output: `0.02 0.10 0.19 1/1498 9159`. Fields 1–3 are float loads.
```
adb_load1 0.02
adb_load5 0.10
adb_load15 0.19
```
Gauges, no labels. Parse first three whitespace fields as float64.

### 6.3 Uptime — `adb shell cat /proc/uptime`

Real output: `2211.96 8352.28`. Field 1 = uptime seconds.
```
# TYPE adb_uptime_seconds gauge
adb_uptime_seconds 2211.96
```

### 6.4 Per-process — `adb shell ps -A -o PID,RSS,%CPU,NAME`

**Decision: `ps` over `top`.** `ps -A -o PID,RSS,%CPU,NAME` reports **RSS as a raw integer
in kB**, whereas `top -b -n1` reports RES in human units (`334M`, `14G`) requiring
unit-suffix parsing. ps is the reliable, exact contract.

Real output (header + rows, whitespace-padded; header has trailing spaces):
```
   PID    RSS %CPU NAME
     1   9060  0.2 init
  3699 333380  3.6 system_server
  4774 299252  0.1 com.google.android.gms
  4316 298388  0.3 com.google.android.gms.persistent
  4751 253592  6.1 com.spocky.projengmenu
  6477 168372  0.0 com.android.vending:background
     2      0  0.0 [kthreadd]
```
Parsing rules:
- Skip the first line (header: begins with `PID`).
- Split each row on runs of whitespace into **exactly 4 fields**: PID, RSS(kB), %CPU, NAME.
  Use `strings.Fields`; NAME is the 4th field (process/package names contain no spaces here
  — kernel threads render as `[kthreadd]`, background procs as `com.android.vending:background`).
- `rss_bytes = RSS_kB * 1024`. `cpu = %CPU / 100` (ratio; note toybox %CPU is a
  lifetime average, not instantaneous — documented, acceptable).
- **Selection:** sort rows by RSS descending, take top `-collect.top-processes` (default 15).
  Additionally include any row whose NAME exactly matches an entry in
  `-collect.process-include`, even if outside the top-N (dedupe by PID/NAME).
- Rows with `RSS == 0` (kernel threads) are eligible only via the allowlist; the top-N is
  taken from the RSS-sorted list so zero-RSS threads naturally fall out.

```
# TYPE adb_process_memory_rss_bytes gauge
adb_process_memory_rss_bytes{process="system_server"} 3.4138112e+08
# TYPE adb_process_cpu_ratio gauge
adb_process_cpu_ratio{process="system_server"} 0.036
```
Label: `process` = NAME. (One process name may have multiple PIDs, e.g. gms; when the same
NAME appears twice, disambiguate is not required for TV monitoring — sum is NOT taken;
emit the highest-RSS instance and drop duplicates by NAME to avoid label collisions.)

### 6.5 Thermal — `adb shell dumpsys thermalservice`

Real output (relevant blocks):
```
Current temperatures from HAL:
	Temperature{mValue=62.500004, mType=0, mName=CPU0, mStatus=0}
	Temperature{mValue=62.500004, mType=0, mName=CPU1, mStatus=0}
	Temperature{mValue=61.000004, mType=1, mName=GPU, mStatus=0}
```
Parsing rules:
- Locate the line `Current temperatures from HAL:`; parse the indented
  `Temperature{...}` lines that follow until a non-matching line. If that block is absent,
  fall back to the `Cached temperatures:` block.
- Per line extract `mValue` (float °C), `mType` (int), `mName` (string) via regex
  `mValue=([-\d.]+).*mType=(\d+).*mName=([^,}]+)`.
- Map `mType` → `type` label (Android `Temperature.Type`):
  `0=CPU, 1=GPU, 2=BATTERY, 3=SKIN, 4=USB_PORT, 5=POWER_AMPLIFIER, 6=BCL_VOLTAGE,`
  `7=BCL_CURRENT, 8=BCL_PERCENTAGE, 9=NPU`; unknown ⇒ `type="unknown"`.
```
# TYPE adb_thermal_temperature_celsius gauge
adb_thermal_temperature_celsius{name="CPU0",type="CPU"} 62.500004
adb_thermal_temperature_celsius{name="GPU",type="GPU"} 61.000004
```
Labels: `name` = mName, `type` = mapped mType.

### 6.6 Network — `adb shell cat /proc/net/dev`

Real output:
```
Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
  eth0: 4653697    8110    0    0    0     0          0       241  1847905    7085    0    0    0     0       0          0
 wlan0:       0       0    0    0    0     0          0     1093       9    0    ...
    lo:    9388      71    0    0    0     0          0         0     9388      71    ...
```
Parsing rules:
- Skip the first two header lines.
- Split each line on `:` → `iface` (trim spaces) + 16 whitespace-separated counters.
  Receive fields[0..7] = bytes,packets,errs,drop,fifo,frame,compressed,multicast.
  Transmit fields[8..15] = bytes,packets,errs,drop,fifo,colls,carrier,compressed.
- Emit as **counters** with `interface` and `direction` (`rx`/`tx`) labels.

| Metric | rx field | tx field |
|--------|----------|----------|
| `adb_network_bytes_total{interface,direction}` | Receive bytes | Transmit bytes |
| `adb_network_packets_total{interface,direction}` | Receive packets | Transmit packets |
| `adb_network_errs_total{interface,direction}` | Receive errs | Transmit errs |
| `adb_network_drop_total{interface,direction}` | Receive drop | Transmit drop |

Emit all interfaces (including `lo`). Type = `CounterValue`.

### 6.7 Storage — `adb shell df /data /cache`

toybox `df` does **NOT** support `-B1`/`--block-size` (`df: Unknown option 'B1'`). Default
output is in **1K-blocks**; multiply ×1024 → bytes.

Real output:
```
Filesystem            1K-blocks    Used Available Use% Mounted on
/dev/block/mmcblk0p32  12203000 3397576   8657968  29% /data/user/0
/dev/block/mmcblk0p19     61360   10716     48624  19% /cache
```
Parsing rules:
- Skip header (line starting with `Filesystem`).
- `strings.Fields` → [Filesystem, 1K-blocks, Used, Available, Use%, Mounted-on].
- `mountpoint` label = **Mounted-on** column (note: `df /data` resolves to `/data/user/0`;
  use the reported mountpoint verbatim). Bytes = field ×1024.
```
# TYPE adb_filesystem_size_bytes gauge
adb_filesystem_size_bytes{mountpoint="/data/user/0"} 1.2495872e+10
adb_filesystem_used_bytes{mountpoint="/data/user/0"} 3.478997e+09
adb_filesystem_avail_bytes{mountpoint="/data/user/0"} 8.86656e+09
```
Metrics: `adb_filesystem_size_bytes`, `adb_filesystem_used_bytes`,
`adb_filesystem_avail_bytes` (all gauges, `mountpoint` label).

### 6.8 Media playback — `adb shell dumpsys media_session`

Real output (idle example — nothing playing):
```
  Sessions Stack - have 1 sessions:
    Netflix media session com.netflix.ninja/Netflix media session (userId=0)
      ownerPid=5143, ownerUid=10088, userId=0
      package=com.netflix.ninja
      active=false
      state=null
```
Parsing rules:
- Session count: parse `Sessions Stack - have (\d+) sessions:` → `adb_media_session_count`.
  (If the phrase is absent, count `package=` lines within the stack.)
- Walk the block line-by-line; each session is delimited by a `package=<pkg>` line. Track
  the current `package`; when a `state=` line is seen for that session:
  - `state=null` ⇒ no playback metric emitted for that session (device idle).
  - `state=PlaybackState {state=N, position=..., ...}` ⇒ extract integer `N` via
    `state=PlaybackState \{state=(\d+)`; emit gauge = N.
- PlaybackState int values (Android `PlaybackState`): `0=NONE,1=STOPPED,2=PAUSED,3=PLAYING,`
  `4=FAST_FORWARDING,5=REWINDING,6=BUFFERING,7=ERROR,8=CONNECTING,9=SKIP_PREV,10=SKIP_NEXT,`
  `11=SKIP_QUEUE_ITEM`.
```
# TYPE adb_media_session_count gauge
adb_media_session_count 1
# TYPE adb_media_playback_state gauge
adb_media_playback_state{package="com.netflix.ninja"} 3
```
`adb_media_playback_state` is emitted **only** for sessions with a non-null PlaybackState;
value = state int, `package` label. (On the reference device at capture time nothing was
playing, so only `adb_media_session_count 1` was present — this is the expected idle case.)

### 6.9 Foreground app — `adb shell dumpsys activity activities | grep -m1 ResumedActivity`

Real output:
```
  ResumedActivity: ActivityRecord{903e9f8 u0 com.spocky.projengmenu/.ui.home.MainActivity t1554}
```
Parsing rules:
- Regex `ResumedActivity:.*\bu\d+ (\S+?)/` → capture group = `package`
  (`com.spocky.projengmenu`). The remainder after `/` is the activity (optional `activity`
  label, `.ui.home.MainActivity`).
- Emit an info-style gauge, constant value `1`, carrying the package.
```
# TYPE adb_foreground_app_info gauge
adb_foreground_app_info{package="com.spocky.projengmenu",activity=".ui.home.MainActivity"} 1
```
If no `ResumedActivity` line is found, emit nothing (not an error unless the command failed).
`dumpsys window | grep mFocusedApp` is an equivalent fallback source (same ActivityRecord
format) if `activity activities` ever lacks the line.

### 6.10 Scrape meta

```
# TYPE adb_scrape_duration_seconds gauge
adb_scrape_duration_seconds 0.83
# TYPE adb_scrape_collector_success gauge
adb_scrape_collector_success{collector="meminfo"} 1
adb_scrape_collector_success{collector="thermal"} 1
# TYPE adb_scrape_errors_total counter
adb_scrape_errors_total 0
```
- `adb_scrape_duration_seconds`: wall time of the whole `Collect`.
- `adb_scrape_collector_success{collector}`: `1`/`0` per sub-collector, **always emitted for
  every registered collector** (so `0` is visible when one fails). When `adb_up=0`, every
  device collector reports `0`.
- `adb_scrape_errors_total`: process-lifetime counter incremented once per collector failure
  (and per failed reconnect). Lives on the Collector struct, persists across scrapes.

## 7. Error-handling contract

- One collector's failure MUST NOT abort the others. The top-level `Collect` iterates the
  fixed collector list; each is wrapped so a returned error → log at warn, set
  `collector_success{collector}=0`, `adb_scrape_errors_total++`, continue.
- A per-command timeout (`context.DeadlineExceeded`) is treated as a collector error.
- Empty stdout or a parse yielding zero records is a collector error (device answered but
  format unexpected) — except the documented "idle" cases (no media playback, no foreground
  line) which emit fewer/zero series successfully.
- `adb_up=0` path: skip all device collectors, still emit `build_info`, `adb_up`,
  `scrape_duration`, all `collector_success=0`, and the errors counter.

## 8. Reconnect strategy

Before device collectors run, `ensureConnected`:
1. Run `adb -s <addr> get-state`. If stdout trimmed == `device` → connected, proceed.
2. Otherwise run `adb connect <addr>` (best-effort), then re-run `get-state`.
3. If still != `device` → `adb_up=0`, increment errors, skip device collectors.

All three steps go through the same serialized mutex. Never retry in a tight loop; one
reconnect attempt per scrape.

## 9. Verification checklist

| # | Metric family | Source command | Expected when device up |
|---|---------------|----------------|-------------------------|
| 0 | `adb_up`, `adb_exporter_build_info` | `adb get-state` | `adb_up 1`; build_info present |
| 1 | `adb_memory_*_bytes` (7) | `cat /proc/meminfo` | 7 gauges, bytes = kB×1024 |
| 2 | `adb_load1/5/15` | `cat /proc/loadavg` | 3 gauges |
| 3 | `adb_uptime_seconds` | `cat /proc/uptime` | 1 gauge, field 1 |
| 4 | `adb_process_memory_rss_bytes`, `adb_process_cpu_ratio` | `ps -A -o PID,RSS,%CPU,NAME` | top-N + allowlist, `process` label |
| 5 | `adb_thermal_temperature_celsius` | `dumpsys thermalservice` | ≥1 per HAL temp, `name`+`type` labels |
| 6 | `adb_network_*_total` | `cat /proc/net/dev` | per-iface rx+tx counters incl eth0/wlan0/lo |
| 7 | `adb_filesystem_{size,used,avail}_bytes` | `df /data /cache` | per-mount gauges, bytes = 1K×1024 |
| 8 | `adb_media_session_count`, `adb_media_playback_state` | `dumpsys media_session` | count always; state only when playing |
| 9 | `adb_foreground_app_info` | `dumpsys activity activities \| grep -m1 ResumedActivity` | 1 series, `package` label |
| 10 | `adb_scrape_*` | (internal) | duration, per-collector success, errors_total |

## 10. Notes / device surprises

- **toybox `df` rejects `-B1`** — must parse default 1K-blocks and ×1024. `df /data`
  resolves the mount to `/data/user/0`, not `/data`.
- **`ps` RSS is raw kB** (clean integer); `top -b -n1` RES is human-formatted (`334M`,
  `14G`) — ps chosen for exact parsing.
- **`%CPU` from toybox ps is a lifetime average**, not an instantaneous sample; exposed as
  `adb_process_cpu_ratio` (÷100). Acceptable for TV monitoring.
- Process NAME can contain `:` (e.g. `com.android.vending:background`) and `[]` (kernel
  threads); it never contains spaces, so 4-field splitting is safe.
- `dumpsys media_session` renders idle sessions as `state=null` — expected; playback state
  only appears as `state=PlaybackState {state=N ...}` while media is active.
- SwapTotal is non-zero (524284 kB zram) on this device — swap metrics are meaningful.
