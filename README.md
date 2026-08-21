# status

A terminal dashboard that plots local system resources, the latency of a remote
endpoint, and the CPU share of the services running on the machine — on shared
ASCII timelines, driven by Claude-style slash commands typed inside the TUI.

Everything relevant is on one screen on purpose: the point is to watch local
pressure, remote latency and per-service load at the same time and see which one
moved first.

```
 STATUS  ·  MAIN(9)/SERVICES(4)  ·  https://status.azion.app/  ·  checks 5s  ·  sys 1s   ▲ 1 ALERT  09:24:34

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

`Shift+Tab` switches to the services screen:

```
 STATUS  ·  MAIN(9)/SERVICES(4)  ·  https://status.azion.app/  ·  checks 5s  ·  sys 1s   ▲ 1 ALERT (+2 unseen)

 SERVICES ─────────────────────────────────────────────────────────────────────────────────
 ● NODE                ⌃100% │                    ▁▃▃▄▄▄▃▂▁                    ▁▁▃▃▄▄▄▃▂▁
   32.4%           thr 50.0% │                ▂▄▆███████████▆▄▂             ▂▄▆███████████
   avg 59% · max 93%         │            ▁▃▅██████████████████▇▅▂▁     ▁▃▅█████████████████
   3 pid · 4.96 cores · 892M │ ███▇▅▄▃▂▂▁▂▂▃▄▆████████████████████████▄▆████████████████████
                             │ ████████████████████████████████████████████████████████████
 ● REDIS-SERVER        ⌃100% │                                                       ░░░░░
   FAIL            thr 50.0% │                                                       ░░░░░
   avg 4% · max 6%        ✕5 │ ╌  ╌  ╌  ╌  ╌  ╌  ╌  ╌  ╌  ╌  ╌  ╌  ╌  ╌  ╌  ╌  ╌  ╌ ░░░░░
   ✕ no process matching "r… │ ▁▁▁▁▁▁▁▁▁▁▂▂▂▂▂▂▂▂▂▂▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁▁░░░░░
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

**Services** — process groups you pick, on the second screen:

| Chart | Source | Notes |
| --- | --- | --- |
| one per service | CPU share of every matching process | detail line shows pid count, cores and RSS |

Nothing is charted there until you add something. `/discover` scans the machine
and lets you pick from a ranked list; `/service add` does it by hand; `/service`
lists what is charted and lets you take things off the list.

Use `/show` to hide the charts you are not watching; hidden charts keep
collecting, so unhiding one restores its history instead of starting blank, and
an alert on a hidden chart — or on the screen you are not looking at — is still
counted in the top bar.

Local metrics are sampled on their own faster cadence (`/sysinterval`, default
1s); the four network checks run on the configurable check interval
(`/interval`, default 5s).

## Running it

```sh
make run          # or: go build -o status . && ./status
```

Go 1.25+ is required (a dependency sets the floor).

```
usage: status [--conf <file.toml>] [--no-save]

  -c, --conf <file>  read and write this configuration instead of the default
      --no-save      do not write the configuration back on exit
  -v, --version      print the version
  -h, --help         print this
```

That is the whole command line: the target, the intervals, the timeouts, the
thresholds and the services are all changed from inside the TUI, so you can
retune while you are watching, and what you set is there next time.

## Packaging a release

```sh
make dist                            # this platform
make dist GOOS=linux GOARCH=amd64    # another one
make dist-all                        # darwin and linux, amd64 and arm64
```

Each run writes `dist/status_<version>_<os>_<arch>.tar.gz` plus a `.sha256`
alongside it. The tarball unpacks into its own directory holding the binary and
this README, so it will not scatter files into whatever directory it is opened
in.

The version comes from `git describe --tags --always --dirty`, so a tagged commit
yields the tag and anything else the short hash with `-dirty` when the tree has
uncommitted changes. It is compiled in, and the binary reports it:

```sh
$ ./status --version
status v1.2.0
```

Release binaries are built with `-trimpath -ldflags "-s -w"`, which keeps local
filesystem paths out of the artifact and drops the symbol and DWARF tables —
about a third of the size, 7.9M down to 3.0M compressed. Cross-compiled builds
have cgo disabled, since there is no cross toolchain; the only behavioural
consequence is that the DNS check's fallback to the platform resolver is Go's own
resolver on those binaries.

Override `VERSION` to name an artifact yourself, which is what a CI tag build
wants:

```sh
make dist-all VERSION=v1.2.0
```

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
| `/threshold <chart> <value>` | alert level that turns a chart red (`0` disables) |
| `/thresholds` | show every configured threshold |
| `/show [chart\|section\|all\|none]` | pick which charts are displayed; no argument opens the picker |
| `/screen [main\|services]` | switch screens; no argument cycles, as `Shift+Tab` does |
| `/discover [filter]` | scan the machine for services and pick which to chart |
| `/service [add\|rm] [name] [match]` | list the charted services, and pick any to stop charting |
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

Keys: `Shift+Tab` switch screens · `Tab` complete · `↑`/`↓` completions, or
command history when the popup is closed · `Esc` dismiss a panel or clear the
prompt · `Ctrl+R` sample everything now · `Ctrl+L` clear history · `Ctrl+C` quit.

The modal lists share their keys: `↑`/`↓` (or `j`/`k`) move, `Space` (or `x`)
ticks the row under the cursor, `Esc` (or `q`) closes.

| List | Tick means | `Enter` | Extra |
| --- | --- | --- | --- |
| `/show` | shown | close | `a` show all, `n` hide all — toggles apply at once |
| `/discover` | chart this | chart the ticked | `l` tick everything listening |
| `/service` | stop charting this | remove the ticked | `a` tick everything |

`/show` applies each toggle immediately; the other two do nothing until `Enter`,
so a mis-hit is harmless. All three own the keyboard while open, so nothing leaks
into the prompt behind them, and the prompt greys out to say so.

A panel taller than the terminal (`/help`, `/config`) scrolls with `↑`/`↓` and
`PgUp`/`PgDn`. Any other key closes it, and that key is swallowed rather than
typed onto the prompt — except `/`, which takes you straight from reading to
typing the next command.

### Value formats

`/host` takes a bare hostname, a `host:port`, or a full URL with a path
(`status.azion.app`, `status.azion.app/health`, `http://localhost:8080`).

Durations take a unit (`500ms`, `2s`, `1m`); a bare number means seconds.

`/threshold` and `/show` take any chart name: a built-in metric (`cpu`, `ttfb`)
or a service (`nginx`, or the fully qualified `service:nginx`). A service cannot
shadow a built-in — a service literally called `cpu` still resolves to
`service:cpu`.

Arguments are split like a shell would, so quotes hold a value together:
`/service add api "java -jar api.jar"`.

Thresholds are read in the unit of their chart:

| Metric | Unit | Accepted | Default |
| --- | --- | --- | --- |
| `cpu`, `mem`, `disk` | percent | `85`, `85%` | 85, 90, 90 |
| `diskio`, `net` | bytes/s | `100MB/s`, `512K`, `1G` | 100M/s |
| `dns`, `ping`, `ttfb`, `request` | milliseconds | `300`, `300ms`, `1.5s` | 100, 100, 300, 800 |
| services | percent of total CPU | `50`, `50%` | 50 |

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

## Services

Nothing is charted on the services screen until you say so.

```
/discover              scan everything
/discover postgres     scan, keeping only names or command lines matching this
/service               list what is charted, and pick any to stop charting
/service add nginx     chart every process whose name contains "nginx"
/service add api "java -jar api.jar"
/service rm nginx      remove one by name, without the list
```

`/service` on its own is the way in: it lists what is charted with each one's
match, threshold and current value, and ticking a row marks it to stop being
charted. Nothing happens until `Enter` confirms, so a mis-hit `Space` costs
nothing, and `Esc` discards the marks. `a` marks everything, for tearing down a
whole set.

The tick means *remove* here, the opposite of the `/show` picker where it means
*shown*, so it is drawn as a red `✗` and the heading says which one you are in.

**How discovery decides what to offer.** "Which processes are services" is not a
question the OS answers directly, and neither launchd nor systemd covers the
containers and dev servers people actually watch. So the heuristic is a listening
socket: a process that accepts connections is a service, and those are ranked
first and marked `◆`. Everything else is ranked by CPU, because that is the other
reason you would chart something. Ports come from the OS connection table, which
on macOS goes through `lsof`; if that is unavailable, discovery still works and
simply offers an unmarked list. Candidates you already chart are flagged so the
list does not invite duplicates.

**How a service is matched.** A case-insensitive substring, tested against the
process name and — for a match containing a space or a `/` — against the full
command line too, which is what it takes to tell two JVMs apart. Every matching
process is summed, so a five-worker nginx is one chart.

**What the number means.** The share of the whole machine's CPU capacity, on the
same 0–100 scale as the `CPU` chart, so the two can be read against each other.
The detail line also gives the raw figure in cores, which is what `top` would
show you: `3 pid · 4.96 cores · 892M`.

The rate comes from diffing the processes' cumulative CPU time counters between
samples. gopsutil's own `CPUPercent` averages over each process's whole lifetime
instead, which would draw a flat line straight through a spike. A service whose
processes have gone away charts as a failed check rather than as 0%, so an outage
is not indistinguishable from an idle service.

Services are sampled on the system cadence (`/sysinterval`). One pass over the
process table costs about 30ms on a machine with 600 processes, and it is skipped
entirely when no services are configured.

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

TOML, read at startup and written back on exit:

```
$XDG_CONFIG_HOME/status-tui/config.toml   # or ~/.config/status-tui/config.toml
```

Whatever you set in the TUI — the target, the intervals, the timeouts, the
thresholds, the services, which charts are shown — is there the next time you
start. `/save` writes it immediately if you want to be sure; otherwise quitting
does it.

Nothing is written when nothing changed, so a file you maintain by hand keeps its
formatting and its timestamp across runs that only looked at it. A first run does
leave a file, even an unchanged one, so there is something to edit.

### Using a specific file

```sh
status --conf ./prod.toml      # read this file, and write it back on exit
status --conf ./prod.toml --no-save
```

A `--conf` path is taken literally: it is read and written, and the default
location is not consulted. That is how you keep one setup per environment — a
`prod.toml` watching a production endpoint with its own thresholds, a
`local.toml` watching a dev server. A file that does not exist yet is created on
exit, so `--conf ./new.toml` is also how you start one.

`--no-save` runs without writing anything back, for when you want to poke at
thresholds without committing to them.

### The format

Every field is optional. What the file omits keeps its default, so a three-line
file is a valid file:

```toml
[target]
url = "https://status.azion.app/health"
ping_mode = "auto"       # icmp, tcp or auto
insecure_tls = false

[sampling]
checks = "5s"            # network check interval
system = "1s"            # local metric interval
history = 600            # samples kept per chart

[sources]
mount = "/"              # filesystem for the disk usage chart
interface = ""           # network interface, empty for all

[timeouts]
dns = "2s"
ping = "2s"
ttfb = "10s"
request = "10s"

# cpu/mem/disk are percent, diskio/net are bytes per second,
# dns/ping/ttfb/request are milliseconds, services are percent of total CPU.
[thresholds]
cpu = 85.0
ttfb = 300.0
"service:nginx" = 25.0
service_default = 50.0   # given to a newly added service

[charts]
cpu = true
disk = false
"service:nginx" = true

[[services]]
name = "nginx"
match = "nginx"          # case-insensitive substring of the process name

[[services]]
name = "api"
match = "java -jar api.jar"
cmdline = true           # also match against the full command line
```

`[charts]` is written out in full on every save — one explicit `true`/`false` per
chart, including the ones left at the default — so the file doubles as the list of
what exists and can be edited without guessing at names. A chart missing from the
map is shown, so adding a service or upgrading to a new built-in never arrives
hidden.

The file is written with the comments above included, because nothing in
`diskio = 104857600` tells you the unit and a file meant to be opened in an
editor should say.

### When it goes wrong

A missing file is normal: the defaults are used and the file is created on exit.

A file that does not parse is reported on stderr and the dashboard starts on the
defaults rather than refusing to open — a stray bracket should not cost you the
tool. That run will not overwrite the file, so you can go and fix it. If you do
change something and it gets written, the unreadable original is copied to
`config.toml.bak` first and the path is printed.

Saves go through a temporary file in the same directory and are renamed over the
target, so an interrupted save cannot leave a half-written config where a good one
used to be.

A `config.json` from an earlier version is imported once, on the first run that
finds no `config.toml` next to it. The JSON file is left alone rather than
deleted, and the import is announced on stderr.

## Layout

Two screens. `MACHINE` and `NETWORK` on the main one, `SERVICES` on the second,
switched with `Shift+Tab`. Switching is a change of view and nothing else: every
chart keeps collecting on both screens, so nothing is lost and nothing restarts
from blank. The top bar shows both screens with their chart counts, and an alert
on the screen you are not looking at is reported as `(+1 unseen)`.

One row per chart, grouped under its section heading. Each row is a fixed
30-column information block — status dot and title, the current value and its
threshold, the window's `avg`/`max`, and the context detail — with the timeline
chart filling every remaining column. Everything lines up in a single table, so
you read one column of numbers and one column of shapes rather than hunting
across tiles.

The information block needs 5 lines. The bottom line of a row is deliberately
left blank in the chart column: with nine area charts touching, the screen reads
as one solid block. The vertical rule marks where the chart begins and is drawn
only beside the chart's own rows, which is what visually groups each chart.

Rows share the available height evenly. A short terminal gives each row fewer
lines rather than dropping a chart — the whole point is that all of them are on
screen at once — shedding the least useful line first:

| Lines per row | Information block |
| --- | --- |
| 5 or more | title · value + threshold · avg/max · detail (2 lines) |
| 4 | title · value + threshold · avg/max · detail |
| 3 | title · value + threshold · detail |
| 2 | title + value · detail |
| 1 | title + value, with an inline sparkline |

A row can also grow *past* the block, up to 12 lines, and the surplus goes to the
chart: a services screen with four charts should spend a tall terminal on
vertical resolution rather than leave a third of it blank.

The minimum size depends on what is on screen, so `/show` is a real way to fit a
short terminal rather than a dead end: nine charts and two headings need 34x15.
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
  zero rate rather than an enormous spike. The same applies to a service whose
  process set changed under the sampler.
- Service discovery and sampling read only what the OS will tell this user about;
  processes owned by others may report a name but no CPU or memory.

## Development

```sh
make test              # unit tests, plus a few that hit the real network
make race
make lint              # go vet plus a gofmt check
go test -short ./...   # skips the network- and process-dependent tests
```

`TestDumpView*` in `internal/ui` print rendered frames at various terminal sizes,
which is the quickest way to check a layout change:

```sh
go test ./internal/ui/ -run TestDumpView -v
```
