mping
=====

![CI](https://github.com/servak/mping/actions/workflows/ci.yml/badge.svg)

**mping** is a `ping` built for network operators: sweep entire subnets, mix ICMP/HTTP/TCP/DNS/NTP checks in a single command, and watch live pass/fail stats in a terminal dashboard — right from the SSH session you're already in.

![Demo](demo/mping.gif)

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

Notes:
- Loss at an intermediate hop that does not continue to the destination is usually ICMP rate limiting on that router, not real loss; the path view points this out.
- Raw sockets are used when available (root / `cap_net_raw`). Otherwise mping falls back to unprivileged ICMP sockets: on macOS this works fully, on Linux only the destination may be visible.
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
    probe: http
    http:
      expect_codes: "200-299"
      expect_body: ""
      headers: {}
      redirect_off: false
      tls:
        skip_verify: true         # Skip TLS certificate verification
  
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
```

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
