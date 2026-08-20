# status

A terminal dashboard that plots local system resources and the latency of a
remote endpoint on the same screen, on shared ASCII timelines, and is driven by
Claude-style slash commands typed inside the TUI.

Everything is on one screen on purpose: the point is to watch local pressure and
remote latency at the same time and see which one moved first.

```
 STATUS  ·  https://status.azion.app/  ·  checks 5s  ·  sys 1s  ·  window 1m40s/8m20s        ▲ 1 ALERT  09:24:34

 MACHINE ──────────────────────────────────────────────────────────────────────────────────
 ● CPU                 ⌃100% │ ╌  ▁▁▂▂▃▃▄▃▃▃▂▂▁▁ ╌  ╌  ╌  ╌  ╌  ╌ ▁▁▂▂▃▃▃▄▃▃▂▂▁▁  ╌  ╌  ╌
   31.4%           thr 85.0% │ ▆▇▇█████████████████▇▆▅▅▄▄▃▃▃▃▃▄▄▅▆▆▇█████████████████▇▆▆▅
   avg 48% · max 72%         │ ██████████████████████████▇▇▆▆▆▆▆▇█████████████████████████
   8 cores                   │ ████████████████████████████████████████████████████████████
   load 2.31 1.98 1.75
 ...
 NETWORK ──────────────────────────────────────────────────────────────────────────────────
 ● TTFB                ⌃1.2s │
   900ms           thr 300ms │                                        ████████████████████
   avg 269ms · max 900ms     │                                        ████████████████████
   dns 2.56ms · conn 16.4ms  │ ▃▃▄▄▄▅▅▅▅▅▄▄▄▃▃▂▂▂▁▁▁▂▂▂▃▃▄▄▅▅▅▅▅▄▄▄▃▃████████████████████
   tls 48.0ms
 ╭──────────────────────────────────────────────────────────────────────────────────────────╮
 │ ❯ /interval 2s                                                                           │
 ╰──────────────────────────────────────────────────────────────────────────────────────────╯
   /interval <duration>   how often the network checks run (default 5s)
```

## What it measures

Nine charts, all visible at once, all plotted as timelines, in two sections.

**Machine** — local resources:

| Chart | Source | Notes |
| --- | --- | --- |
| `CPU` | total CPU utilisation | detail line shows core count and load averages |
| `MEMORY` | used / total physical memory | |
| `DISK USAGE` | used percentage of one mountpoint | pick it with `/disk` |
| `DISK I/O` | read + write bytes/s | detail line splits read and write |

**Network** — the remote target:

| Chart | Source | Notes |
| --- | --- | --- |
| `THROUGHPUT` | rx + tx bytes/s | all interfaces, or one via `/net` |
| `PING` | round-trip latency to the target | ICMP echo, falling back to a TCP handshake |
| `DNS LOOKUP` | time to resolve the target hostname | see *How the checks work* |
| `TTFB` | time to the first response byte | |
| `REQUEST` | time to a fully read response | same request as TTFB |

Use `/show` to hide the charts you are not watching; hidden charts keep
collecting, so unhiding one restores its history instead of starting blank, and
an alert on a hidden chart is still counted in the top bar.

Local metrics are sampled on their own faster cadence (`/sysinterval`, default
1s); the four network checks run on the configurable check interval
(`/interval`, default 5s).

## Running it

```sh
go build -o status .
./status
```

Go 1.25+ is required (a dependency sets the floor). No arguments and no flags:
the target, the intervals, the timeouts and the thresholds are all changed from
inside the TUI, so you can retune while you are watching.

## Commands

Type `/` to open the completion popup. `Tab` accepts the highlighted entry;
`Enter` runs the command, completing it first if it is still a prefix.

| Command | Description |
| --- | --- |
| `/help` | list every command and keybinding |
| `/host <domain\|url>` | change the target of the network checks |
| `/interval <duration>` | how often the network checks run |
| `/sysinterval <duration>` | how often local system metrics are sampled |
| `/timeout <dns\|ping\|ttfb\|request\|all> <duration>` | set a per-check timeout |
| `/timeouts` | show every configured timeout |
| `/threshold <metric> <value>` | alert level that turns a chart red (`0` disables) |
| `/thresholds` | show every configured threshold |
| `/show [metric\|section\|all\|none]` | pick which charts are displayed; no argument opens the picker |
| `/disk <mountpoint>` | which filesystem the disk usage panel reports |
| `/net <interface\|all>` | which interface the network panel sums |
| `/pingmode <auto\|icmp\|tcp>` | latency transport |
| `/history <samples>` | how many samples to keep per series |
| `/insecure [on\|off]` | skip TLS certificate verification |
| `/pause`, `/resume` | stop and restart collection, keeping the history |
| `/clear` | discard all chart history |
| `/config` | show the full current configuration |
| `/save` | persist the configuration to disk |
| `/reset` | restore built-in defaults |
| `/quit` | exit |

Keys: `Tab` complete · `↑`/`↓` completions, or command history when the popup is
closed · `Esc` dismiss a panel or clear the prompt · `Ctrl+R` run the checks now
· `Ctrl+L` clear history · `Ctrl+C` quit.

In the `/show` picker: `↑`/`↓` move · `Space` toggle · `a` show all · `n` hide
all · `Esc` close. The picker owns the keyboard while it is open, so nothing
leaks into the prompt behind it.

### Value formats

`/host` takes a bare hostname, a `host:port`, or a full URL with a path
(`status.azion.app`, `status.azion.app/health`, `http://localhost:8080`).

Durations take a unit (`500ms`, `2s`, `1m`); a bare number means seconds.

Thresholds are read in the unit of their metric:

| Metric | Unit | Accepted | Default |
| --- | --- | --- | --- |
| `cpu`, `mem`, `disk` | percent | `85`, `85%` | 85, 90, 90 |
| `diskio`, `net` | bytes/s | `100MB/s`, `512K`, `1G` | 100M/s |
| `dns`, `ping`, `ttfb`, `request` | milliseconds | `300`, `300ms`, `1.5s` | 100, 100, 300, 800 |

## Reading the charts

Time flows left to right; the rightmost column is the newest sample. Column
height is the value against the axis maximum shown in the header (`⌃100%`).

| Glyph | Meaning |
| --- | --- |
| `▁`–`█` | the value, in eighths of a cell |
| `╌` | the threshold guide line |
| `░` (red) | a check that failed outright — timeout, DNS error, HTTP ≥ 400 |
| `·` | a column with no data yet |

When the newest sample breaches its threshold, the whole panel turns red — title,
value and chart body — while the columns that are actually above the line stay a
brighter red, so you can see when the breach started. The top bar counts how many
panels are alerting. The footer keeps the window's `avg` and `max`, and a `✕N`
count of failures.

The vertical axis tracks the data in the window, not the threshold. A 100MB/s
network threshold would otherwise squash normal traffic into a flat line at the
bottom of the chart; when a threshold sits above the visible range its guide line
is simply not drawn, and by the time it matters the breach itself has raised the
scale.

## How the checks work

**DNS.** Measured with Go's own resolver rather than the platform one, because
the platform resolver answers from the OS cache and would report a fraction of a
millisecond regardless of the network. If that resolver cannot work (no usable
`/etc/resolv.conf`) the check retries through the system resolver and says so in
the detail line.

**Ping.** ICMP echo over an unprivileged datagram socket, so no root and no
setuid binary is needed on macOS, or on Linux hosts where
`net.ipv4.ping_group_range` includes your gid. When ICMP is unavailable or
filtered the check falls back to timing a TCP handshake against the target port
and labels itself `tcp` — the detail line always says which transport produced
the number. Force one with `/pingmode`.

**TTFB and request time.** One HTTP GET produces both, so the two numbers always
describe the same transaction. Keep-alive is disabled, so every check pays a
fresh DNS, TCP and TLS cost and successive samples stay comparable; the TTFB
detail line breaks the connection down into its `dns`, `conn` and `tls` phases.
The two timeouts bound different phases: the TTFB timeout bounds the wait for
response headers, the request timeout bounds the whole exchange including the
body. An HTTP status of 400 or above counts as a failed check.

## Configuration file

Read at startup and written by `/save`:

```
$XDG_CONFIG_HOME/status-tui/config.json   # or ~/.config/status-tui/config.json
```

It holds the target, both intervals, the history depth, the timeouts, the
thresholds and the list of charts `/show` has switched off.

A missing file is normal. An unparsable one is reported on stderr and the
dashboard starts on the defaults rather than refusing to run.

## Layout

One row per chart, grouped under a `MACHINE` and a `NETWORK` heading. Each row is
a fixed 30-column information block — status dot and title, the current value and
its threshold, the window's `avg`/`max`, and the context detail — with the
timeline chart filling every remaining column. Everything lines up in a single
table, so you read one column of numbers and one column of shapes rather than
hunting across tiles.

A row is 5 lines by default. The bottom line is deliberately left blank in the
chart column: with nine area charts touching, the screen reads as one solid
block. The vertical rule marks where the chart begins and is drawn only beside
the chart's own rows, which is what visually groups each chart.

Nine 5-line rows plus two headings, the top bar and the prompt need about 53
terminal lines. A shorter terminal gets fewer lines per row rather than fewer
charts — the whole point is that all of them are on screen at once — dropping the
least useful line first:

| Lines per row | Information block |
| --- | --- |
| 5 | title · value + threshold · avg/max · detail (2 lines) |
| 4 | title · value + threshold · avg/max · detail |
| 3 | title · value + threshold · detail |
| 2 | title + value · detail |
| 1 | title + value, with an inline sparkline |

The minimum size depends on what is on screen, so `/show` is a real way to fit a
short terminal rather than a dead end: nine charts need 34x15, five need 34x10.
Below that the dashboard says so instead of quietly hiding some. The prompt stays
pinned to the bottom.

Because the chart takes everything past column 30, a wider terminal buys a
longer visible time window rather than bigger tiles; the top bar reports the
window as `system/checks`.

## Notes

- On macOS, `/sysinterval` below roughly 500ms makes the CPU reading quantise —
  the kernel's CPU tick counters are too coarse for the window, and an idle
  machine can read 0.0%. The default of 1s avoids this.
- `DISK I/O` sums every device the OS reports, including synthetic volumes.
- A counter that goes backwards (device removed, wraparound) is reported as a
  zero rate rather than an enormous spike.

## Development

```sh
go test ./...          # unit tests, plus one that hits the real network
go test -race ./...
go test -short ./...   # skips the network-dependent test
go vet ./...
```

`TestDumpView*` in `internal/ui` print rendered frames at various terminal sizes,
which is the quickest way to check a layout change:

```sh
go test ./internal/ui/ -run TestDumpView -v
```
