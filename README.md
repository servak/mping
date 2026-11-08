mping
=====

![CI](https://github.com/servak/mping/actions/workflows/ci.yml/badge.svg)

**mping** is a `ping` built for network operators: sweep entire subnets, mix ICMP/HTTP/TCP/DNS/NTP checks in a single command, and watch live pass/fail stats in a terminal dashboard — right from the SSH session you're already in.

![ICMP demo: history sparklines, a host going DOWN with a live outage timer, and the outage timeline printed on exit](demo/icmp.gif)

## Features

- 🧰 **Built for network ops**: CIDR subnet sweeps, mixed-protocol checks, and a terminal-native dashboard for SSH sessions and jump hosts
- 🎯 **Multi-target monitoring**: Monitor dozens of hosts simultaneously
- 🌐 **Multi-protocol support**: ICMP, HTTP/HTTPS, TCP, DNS, and NTP monitoring
- 📊 **Real-time statistics**: Live success rates, response times, and packet loss
- 🖥️ **Interactive TUI**: Clean terminal interface with sortable results
- ⚡ **High performance**: Concurrent probing with configurable intervals
- 📁 **Batch mode**: Non-interactive mode for scripting and automation, with JSON/CSV output and exit codes
- ⏱️ **Outage measurement**: Records every outage with start/end time and duration — ideal for failover and maintenance tests
- 🛣️ **Path trace (MTR)**: Press `p` in the TUI to see per-hop loss and latency to the selected host
- 📈 **History at a glance**: Sparkline of recent probes and RTT p50/p95/p99 per host

## Supported Protocols

| Protocol | Format | Example | Description |
|----------|--------|---------|-------------|
| **ICMP v4** | `hostname`, `subnet`, or `icmpv4://hostname` | `8.8.8.8`, `192.168.1.0/24`, `icmpv4://google.com` | Traditional ping functionality with subnet support |
| **ICMP v6** | `icmpv6://hostname` | `icmpv6://google.com` | IPv6 ping support |
| **HTTP/HTTPS** | `http://url` or `https://url` | `https://google.com` | Web service monitoring |
| **TCP** | `tcp://host:port` | `tcp://google.com:443` | Port connectivity testing |
| **DNS** | `dns://[server[:port]]/domain[/record_type]` | `dns://8.8.8.8/google.com/A`, `dns:///google.com` | DNS query monitoring |
| **NTP** | `ntp://[server[:port]]` | `ntp://pool.ntp.org`, `ntp://time.google.com:123` | Network Time Protocol monitoring |

Mix any of them in a single command and compare them side by side — the gateway, a web service, a TCP port, a DNS resolver and an NTP server:

![Multi-protocol demo: ICMP, HTTPS, TCP, DNS and NTP checks in one dashboard](demo/multi-protocol.gif)

## Install

https://github.com/servak/mping/releases

## Build

```
go install github.com/servak/mping/cmd/mping@latest
```
## Permission

```
sudo setcap cap_net_raw=+ep mping
```

or

```
sudo chown root mping
sudo chmod u+s mping
```

## Quick Start

### Basic usage
```bash
# Monitor multiple hosts with ICMP
mping 8.8.8.8 1.1.1.1 google.com

# Monitor entire subnet ranges
mping 192.168.1.0/24 10.0.0.0/29

# Mix different protocols
mping google.com https://google.com tcp://google.com:443

# Monitor web services
mping https://github.com https://google.com http://httpbin.org

# IPv6 support
mping icmpv6://google.com icmpv6://2001:4860:4860::8888

# DNS monitoring with explicit servers
mping dns://8.8.8.8/google.com/A dns://1.1.1.1/cloudflare.com/AAAA

# DNS monitoring with defaults (uses 8.8.8.8:53, A record)
mping "dns:///google.com" "dns:///github.com"

# NTP time synchronization monitoring
mping ntp://pool.ntp.org ntp://time.google.com:123

# Mixed protocol monitoring
mping google.com https://google.com ntp://time.google.com
```

### Batch mode for automation
```bash
# Run 10 probes and exit
mping batch --count 10 google.com https://api.example.com

# Monitor subnet ranges in batch mode
mping batch --count 5 192.168.1.0/29

# Use with external host lists
mping batch -f hosts.txt --count 5

# Machine-readable output (json / csv) for scripts and dashboards
mping batch -o json --count 5 10.0.0.0/29 | jq '.[] | select(.loss_percent > 0) | .host'
mping batch -o csv --count 10 -f hosts.txt > result.csv

# Health check: exit with status 2 if any target's loss exceeds 20%
mping batch --max-loss 20 -f hosts.txt || alert "some targets are unhealthy"
```

Batch mode details:
- `--count N` sends exactly N probes per target.
- `-o, --output` selects `table` (default), `json`, or `csv`. RTT fields are in milliseconds and include `jitter_ms` (standard deviation of RTT, same as `mdev` in iputils ping).
- `--max-loss PCT` turns batch into a health check. Exit codes: `0` = OK, `1` = usage/config error, `2` = at least one target exceeded the threshold (violating targets are listed on stderr).
- Progress dots are written to stderr only when it is a terminal, so stdout stays clean for pipes. The failure beep is disabled in batch mode.
- `-I, --interface` selects the source interface, same as the interactive mode.

## Outage Measurement

mping turns consecutive failures into **outages** and records when each one started and ended. This answers the question that matters during failover tests and maintenance windows: *how long was it down?*

- An outage starts when **3 or more consecutive probes** are lost (shorter runs count as isolated packet loss) and ends at the next successful probe. Its start is the send time of the first lost probe.
- Duration is `lost probes × interval`, so use a short interval (e.g. `-i 100`) for sub-second precision.
- The host list's **LastFailTime** column also shows the outage duration: `16:44:20 (DOWN 5.20s)` (red, counting up) while a host is down, and `16:44:15 (0.50s)` (yellow) once it has recovered.
- The TUI host detail panel (`v`) shows the outage count, total downtime, the longest outage, and the most recent outages.
- When you quit the TUI (and in `mping batch`), a timeline of all outages across targets is printed:

```
┌──────────────────────────────────────────────────────────────────────────────────┐
│ Outages (2)                                                                      │
├──────────────┬──────────────┬──────────┬──────┬─────────────────────┬────────────┤
│ START        │ END          │ DURATION │ LOST │ HOST                │ LAST ERROR │
├──────────────┼──────────────┼──────────┼──────┼─────────────────────┼────────────┤
│ 14:03:05.120 │ 14:03:41.870 │  36.750s │  368 │ core-sw1(10.0.0.1)  │ timeout    │
│ 14:03:06.002 │ (ongoing)    │   1.010s │   10 │ 10.0.0.254          │ timeout    │
└──────────────┴──────────────┴──────────┴──────┴─────────────────────┴────────────┘
```

- `mping batch -o json` includes `outages[]`, `outage_count`, `total_downtime_ms`, and `longest_outage_ms` per target.
- Probe results can arrive out of order (a timeout is reported after a later probe already succeeded), so results are applied in send order after `timeout + interval`. Outages therefore appear in the TUI with that delay.

## Path Trace (MTR)

Press `p` in the TUI to show a live, MTR-style trace of the path to the selected host. The trace follows the selection. Any target works — for `tcp://`, `https://`, `dns://`, … the host part is traced.

The trace uses the protocol that matches how the selected probe's packets are forwarded, and the header shows it (`1.1.1.1 · udp · 42 rounds`):

| Selected target | Trace | Why |
|-----------------|-------|-----|
| ICMP (`icmpv4`, `icmpv6`, plain hosts, custom ICMP probers) | ICMP echo, fixed echo ID | The ICMP probe is a single flow, so ECMP routers keep it on one branch; so does the trace. |
| HTTP, HTTPS, TCP, DNS, NTP (and custom probers of those kinds) | UDP, traceroute style | These probes open a new source port per probe, and routers hash ECMP on the L4 ports, so the probe's traffic spreads over every branch. |

UDP probes are sent from one ephemeral source port to destination ports `33434 + (33 × round + ttl) mod 64`: each hop cycles through 64 flows (and then repeats them, so per-branch statistics accumulate), and two consecutive rounds never share a port. Routers answer with Time Exceeded, the destination with Port Unreachable. Every branch shows up as its own responder row under the hop, so a UDP trace through an ECMP fabric typically lists several routers per hop where an ICMP trace (or `mtr`, `traceroute -I`) shows one.

Privileges needed for the trace:

| Platform | ICMP trace | UDP trace |
|----------|------------|-----------|
| root / `cap_net_raw` (Linux, macOS) | raw ICMP socket | UDP socket + raw ICMP socket for the replies |
| Linux, unprivileged | datagram ICMP if `net.ipv4.ping_group_range` allows it; intermediate hops are not visible | UDP socket error queue (`IP_RECVERR`): all hops visible, no privileges needed |
| macOS, unprivileged | datagram ICMP, all hops visible | UDP socket + datagram ICMP socket, all hops visible |

Notes:
- When several routers answer for the same hop (ECMP / load balancing), each one is listed under the hop with its share of the replies and its own Last/Avg/Best/Wrst; the first row is the most frequent responder. Responders that stop answering are dimmed and marked `(stale)`, and are hidden after a longer silence once another router has answered more recently (the most recent responder of a hop is never hidden, so a rate-limited hop keeps its address). At most 4 extra responders are shown per hop, followed by `+N more`.
- Loss at an intermediate hop that does not continue to the destination is usually ICMP rate limiting on that router, not real loss; the path view points this out.
- Raw sockets are used when available (root / `cap_net_raw`), otherwise the unprivileged fallbacks above. When none is available the path view says why instead of showing a trace.
- A destination that actually listens on one of the UDP ports answers with data instead of Port Unreachable; that probe is then lost. UDP to `33434-33497` is rarely open, but firewalls that drop UDP or ICMP errors hide the hops behind them.
- In the TUI, path probing runs at most once per second so that router rate limiting does not show up as fake loss.

## History and Percentiles

The host list has a **History** column with the last 20 probes (oldest on the left). The bar height shows how much slower than usual a probe was: the RTT increase over the host's median, on a fixed millisecond scale. So +20ms looks the same on a 10ms LAN host and on a 150ms overseas host.

| Mark | Meaning |
|------|---------|
| green `▁` | within +5ms of the median |
| green `▂▃▄▅` | +5ms / +10ms / +20ms / +50ms slower |
| yellow `▆▇█` | +100ms / +200ms / +500ms slower |
| red `×` | no reply (timeout / connection failure) |

Latency spikes and loss bursts are visible at a glance. The host details and JSON/CSV output include RTT p50/p95/p99 over the last 100 probes.

## Target Expansion

mping supports powerful target expansion syntax for monitoring large numbers of hosts efficiently:

### Bracket Expansion
Use square brackets `[]` to specify multiple targets with patterns:

#### Numeric Ranges
```bash
# Basic numeric range
mping server[1-5].example.com
# Expands to: server1.example.com, server2.example.com, ..., server5.example.com

# Zero-padded ranges (auto-detected from start format)
mping web[01-10].example.com
# Expands to: web01.example.com, web02.example.com, ..., web10.example.com

# Three-digit padding
mping host[001-100].datacenter.com
# Expands to: host001.datacenter.com, host002.datacenter.com, ..., host100.datacenter.com
```

#### Character Ranges
```bash
# Lowercase letters
mping server[a-d].example.com
# Expands to: servera.example.com, serverb.example.com, serverc.example.com, serverd.example.com

# Uppercase letters
mping db[A-C].cluster.internal
# Expands to: dbA.cluster.internal, dbB.cluster.internal, dbC.cluster.internal
```

#### List Expansion
```bash
# Comma-separated lists
mping service[web,api,db].example.com
# Expands to: serviceweb.example.com, serviceapi.example.com, servicedb.example.com

# Mixed environments
mping app[dev,staging,prod].company.com
# Expands to: appdev.company.com, appstaging.company.com, appprod.company.com
```

#### Multiple Brackets (Combinations)
```bash
# Multiple expansion points
mping server[1-2].[dev,prod].example.com
# Expands to: server1.dev.example.com, server1.prod.example.com, 
#            server2.dev.example.com, server2.prod.example.com

# Protocol-specific monitoring
mping https://api[1-3].[us,eu].service.com/health
```

#### Advanced Use Cases
```bash
# Infrastructure monitoring
mping db[01-05].cluster[a-c].datacenter.com

# Multi-region web services
mping https://[web,api,cdn][1-3].[us-west,eu-central].example.com

# Network equipment
mping switch[01-24].rack[a-f].datacenter.internal
```

### File-based Expansion
Bracket expansion also works in target files (`-f` option):

```bash
# Create targets.txt
cat > targets.txt << EOF
# Web tier
web[01-05].prod.example.com
api[1-3].prod.example.com

# Database tier  
db[01-02].[master,slave].prod.example.com

# Monitoring endpoints
https://health[1-5].prod.example.com/status
EOF

# Monitor all expanded targets
mping -f targets.txt
```

### Combination with CIDR Ranges
Bracket expansion works alongside CIDR notation:

```bash
# Mix bracket expansion with subnet ranges
mping server[1-3].example.com 192.168.1.0/29

# In files
echo "web[01-03].prod.example.com" >> targets.txt
echo "10.0.1.0/28" >> targets.txt
mping -f targets.txt
```

### Key Features
- **Cross-platform**: Works on Windows, macOS, and Linux
- **Shell-independent**: Unlike bash `{}` expansion, works in files and non-bash environments  
- **Zero-padding intelligence**: Automatically detects and maintains padding format
- **Error handling**: Invalid patterns are preserved as-is (e.g., `[5-1]` stays `[5-1]`)
- **Protocol support**: Works with all protocol prefixes (`https://`, `tcp://`, `dns://`, etc.)

## DNS Monitoring Details

### DNS Target Format
```
dns://[server[:port]]/domain[/record_type]
```

- **server**: DNS server IP or hostname (optional, defaults to 8.8.8.8)
- **port**: DNS server port (optional, defaults to 53)
- **domain**: Domain name to query (required)
- **record_type**: DNS record type (optional, defaults to A)

IPv6 servers: write `[2001:db8::1]` or `[2001:db8::1]:5353`. An unbracketed IPv6 address is always read as a bare address (no port), so `dns://2001:db8::1:5353/...` means the address `2001:db8::1:5353` on the default port.

### Supported Record Types
A, AAAA, CNAME, MX, NS, PTR, SOA, SRV, TXT

### DNS Examples
```bash
# Basic A record query using default server (8.8.8.8:53)
mping "dns:///google.com"

# Explicit server and record type
mping dns://1.1.1.1/google.com/AAAA

# Custom port
mping dns://dns-server:5353/example.com/MX

# Multiple DNS queries
mping dns://8.8.8.8/google.com/A dns://1.1.1.1/google.com/AAAA
```

### Default Configuration
DNS queries use these defaults (configurable via ~/.mping.yml):
- Server: 8.8.8.8
- Port: 53
- Record Type: A
- Protocol: UDP

## Usage

```
Usage:
  mping [IP or HOSTNAME]... [flags]
  mping [command]

Examples:
mping 1.1.1.1 8.8.8.8
mping 192.168.1.0/24
mping icmpv6://google.com
mping http://google.com
mping ntp://pool.ntp.org
mping tcp://google.com:443 https://google.com ntp://time.google.com 8.8.8.8

Available Commands:
  batch       Disables TUI and performs probing for a set number of iterations
  config      management config
  help        Help about any command

Flags:
  -c, --config string      config path (default "~/.mping.yml")
  -f, --filename string    use contents of file
  -h, --help               help for mping
  -I, --interface string   source interface (name or IP address)
  -i, --interval int       interval(ms) (default 1000)
  -t, --timeout int        timeout(ms) (default 1000)
  -n, --title string       print title
  -v, --version            Display version

Use "mping [command] --help" for more information about a command.
```

### Keyboard shortcuts (TUI)

| Key | Action |
|-----|--------|
| `j` / `k` / `g` / `G` / `u` / `d` | Move selection |
| `s` / `S` | Next / previous sort key |
| `r` / `R` | Reverse sort / reset metrics |
| `v` / `p` | Toggle detail / path (MTR) view |
| `/` | Filter hosts |
| `t` | Cycle theme |
| `b` | Toggle failure beep globally |
| `m` | Mute/unmute the failure beep for the selected host |
| `h` | Help |
| `q` / `Ctrl+C` | Quit |

### Silencing a known-down target

mping beeps when a probe fails. To stop the beep for a target you already know is down, press `m` on it in the host list (a `⊘` marker is shown next to the host name; press `m` again to restore the beep). Silencing only suppresses the beep: probing, statistics and outage tracking continue as normal, and other hosts still beep.

## Configuration

mping supports flexible configuration through YAML files located at `~/.mping.yml`. This allows you to customize behavior for different protocols and create custom prober profiles.

### Configuration File Structure

```yaml
# Default prober to use for plain hostnames (e.g., when running "mping google.com")
default: icmpv4

# Custom prober configurations
prober:
  # ICMP v4 configuration
  icmpv4:
    probe: icmpv4
    icmp:
      body: "mping"           # ICMP payload (default: "mping")
      tos: 0                  # Type of Service (0-255)
      ttl: 64                 # Time to Live (0-255)
      source_interface: ""    # Source interface name or IP
  
  # ICMP v6 configuration
  icmpv6:
    probe: icmpv6
    icmp:
      body: "mping"
      ttl: 64
      source_interface: ""
  
  # HTTP configuration
  http:
    probe: http
    http:
      expect_codes: "200-299"     # Expected HTTP status codes
      expect_body: ""             # Expected response body (optional)
      headers:                    # Custom headers (optional)
        User-Agent: "mping/1.0"
      redirect_off: false         # Disable redirect following
  
  # HTTPS configuration
  https:
    probe: https
    http:
      expect_codes: "200-299"
      expect_body: ""
      headers: {}
      redirect_off: false
      tls:
        skip_verify: true         # Skip TLS certificate verification (default: true when omitted)
  
  # TCP configuration
  tcp:
    probe: tcp
    tcp:
      source_interface: ""        # Source interface for connections
  
  # DNS configuration
  dns:
    probe: dns
    dns:
      server: "8.8.8.8"          # DNS server IP (required)
      port: 53                   # DNS server port (1-65535)
      record_type: "A"           # Default record type
      use_tcp: false             # Use TCP instead of UDP
      recursion_desired: true    # Enable recursive queries
      expect_codes: ""           # Expected DNS response codes (optional)
  
  # NTP configuration
  ntp:
    probe: ntp
    ntp:
      server: "pool.ntp.org"     # NTP server IP or hostname (required)
      port: 123                  # NTP server port (1-65535, default: 123)
      max_offset: "5s"           # Maximum time offset before alert (e.g., "100ms", "5s")

# UI configuration
ui:
  cui:
    border: true                  # Show border around TUI
```

### Merging with Defaults

Entries under `prober:` whose name matches a built-in prober (`icmpv4`, `icmpv6`, `http`, `https`, `tcp`, `dns`, `ntp`) are **merged** over the built-in defaults field by field. You only need to write the settings you want to change:

```yaml
prober:
  https:
    http:
      expect_codes: "200"   # tls.skip_verify etc. keep their defaults
  dns:
    dns:
      server: "1.1.1.1"     # port 53, record_type A, ... are kept
```

If an entry changes the probe family (e.g. `http: {probe: tcp, tcp: {}}`), it replaces the default instead. Prober entries with new names (custom probers) are used exactly as written.

### HTTP/HTTPS Scheme and TLS Rules

The URL scheme used for HTTP probers is decided as follows:

1. The built-in `https://` prefix always uses HTTPS and `http://` always uses plain HTTP, regardless of `probe:` or `tls:` settings.
2. A custom prefix may carry an explicit scheme: `api-check://https://api.example.com/health` uses HTTPS, `api-check://http://...` uses HTTP.
3. Otherwise (`api-check://api.example.com`), the prober config decides: `probe: https` uses HTTPS; a `tls:` block also implies HTTPS (for backward compatibility); otherwise plain HTTP.

TLS certificate verification is skipped by default. `tls: {}` or omitting `skip_verify` keeps that default; set `skip_verify: false` to verify certificates.

Custom headers are sent with every request. A `Host` header (case-insensitive) overrides the HTTP Host, which is useful for probing a virtual host by IP:

```yaml
prober:
  vhost:
    probe: http
    http:
      headers:
        Host: "www.example.com"
```

```bash
mping vhost://192.0.2.10/ vhost://https://192.0.2.10/
```

### HTTP Status Code Patterns

The `expect_codes` field supports flexible status code matching:

```yaml
# Single status code
expect_codes: "200"

# Status code range
expect_codes: "200-299"

# Multiple specific codes
expect_codes: "200,201,202"

# Mixed patterns
expect_codes: "200,201,300-399,404"

# Empty means accept any status code
expect_codes: ""
```

### DNS Response Code Patterns

DNS queries support similar pattern matching for response codes:

```yaml
# Accept only successful responses
expect_codes: "0"

# Accept successful or non-authoritative responses
expect_codes: "0,5"

# Accept range of codes
expect_codes: "0-5"
```

Common DNS response codes:
- `0`: No error (NOERROR)
- `1`: Format error (FORMERR)
- `2`: Server failure (SERVFAIL)
- `3`: Name error (NXDOMAIN)
- `5`: Refused (REFUSED)

### Custom Prober Names

You can create custom prober configurations with any name:

```yaml
prober:
  # Custom fast HTTP checker
  api-check:
    probe: http
    http:
      expect_codes: "200,201"
      headers:
        Authorization: "Bearer token"
  
  # Custom internal network ICMP
  internal-ping:
    probe: icmpv4
    icmp:
      body: "internal-check"
      source_interface: "eth1"
  
  # Custom strict NTP checker
  ntp-strict:
    probe: ntp
    ntp:
      server: "time.google.com"
      port: 123
      max_offset: "100ms"
```

Use custom probers with the prefix format:
```bash
mping api-check://api.example.com internal-ping://192.168.1.1 ntp-strict://pool.ntp.org

# HTTP custom probers can specify the scheme explicitly
mping api-check://https://api.example.com/health
```

For HTTP custom probers without an explicit scheme, `probe: https` (or a `tls:` block) selects HTTPS; see [HTTP/HTTPS Scheme and TLS Rules](#httphttps-scheme-and-tls-rules).

### Configuration Validation

mping validates configuration files at startup and provides detailed error messages for invalid settings:

```bash
# Example validation errors
$ mping google.com
Error: multiple validation errors: 
  prober 'http': invalid expect_codes pattern: 200-;
  prober 'dns': DNS server is required;
  default prober 'invalid' not found in prober configurations
```

## Development

```bash
go test ./...          # unit tests
go vet -tags 'integration e2e' ./...
```

### Integration and end-to-end tests

These use real sockets on loopback only (`127.0.0.1`, `::1`) plus HTTP/HTTPS/TCP/DNS/NTP servers started by the tests, so they need no external network. CI runs them in the `Integration / E2E` job.

```bash
# Path tracer and TUI path view (integration build tag).
# Without MPING_IT_TARGETS / MPING_IT_HOSTS they probe public hosts.
MPING_IT_TARGETS=127.0.0.1,::1 MPING_IT_ROUNDS=10 MPING_IT_INTERVAL=200ms \
  go test -tags integration -run Integration -v ./internal/trace/
MPING_IT_HOSTS=127.0.0.3,127.0.0.1,127.0.0.2 MPING_IT_DURATION=8s \
  go test -tags integration -run Integration -v ./internal/ui/tui/

# The real binary: "mping batch -o json" against every protocol (e2e build tag).
# Without MPING_BIN the test builds ./cmd/mping itself.
go test -tags e2e -v ./test/e2e/
```

The ICMP prober uses raw sockets, so the ICMP end-to-end cases are skipped unless the binary has `cap_net_raw` (set `MPING_E2E_ICMP=require` to fail instead of skip). On Linux:

```bash
go build -o /tmp/mping ./cmd/mping
sudo setcap cap_net_raw=+ep /tmp/mping
MPING_BIN=/tmp/mping MPING_E2E_ICMP=require go test -tags e2e -v ./test/e2e/

# Raw-socket path of the tracer: grant the compiled test binary, not go test
go test -c -tags integration -o /tmp/trace.test ./internal/trace/
sudo setcap cap_net_raw=+ep /tmp/trace.test
(cd internal/trace && MPING_IT_TARGETS=127.0.0.1,::1 MPING_IT_PRIVILEGED=1 /tmp/trace.test -test.run Integration -test.v)
```

The tracer integration test traces every target with ICMP and with UDP. Without privileges the ICMP trace falls back to datagram ICMP, which Linux only allows when the user's group is within `net.ipv4.ping_group_range` (otherwise the integration tests skip; `MPING_IT_STRICT=1` makes them fail); the UDP trace uses the socket error queue. `MPING_IT_PRIVILEGED=1` / `0` asserts which socket type was used. Linux rate-limits ICMP errors (`net.ipv4.icmp_ratelimit`, `net.ipv6.icmp.ratelimit`); CI sets both to 0.

Real ECMP is tested with Linux network namespaces (root required): `test/netns/ecmp.sh up` builds a client, a router with a two-way multipath route hashed on L4 ports (`fib_multipath_hash_policy=1`), two branch routers and a destination. The ECMP test then asserts that a UDP trace sees both branches at hop 2 and an ICMP trace exactly one:

```bash
go test -c -tags integration -o /tmp/trace.test ./internal/trace/
sudo test/netns/ecmp.sh up
sudo ip netns exec mping-c env MPING_IT_ECMP_TARGET=10.200.0.1 /tmp/trace.test -test.run IntegrationECMP -test.v
sudo test/netns/ecmp.sh down
```

On macOS everything except the ICMP prober cases runs without privileges: the tracer falls back to unprivileged datagram ICMP, and HTTP/HTTPS/TCP/DNS/NTP need none. The ICMP prober cases need `sudo` (setcap does not exist on macOS) and are skipped otherwise.
