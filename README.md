mping
=====

![CI](https://github.com/servak/mping/actions/workflows/ci.yml/badge.svg)

**mping** is a multi-target, multi-protocol network monitoring tool that extends traditional ping functionality. Monitor multiple hosts and services simultaneously with real-time statistics and an interactive terminal UI.

## Features

- 🎯 **Multi-target monitoring**: Monitor dozens of hosts simultaneously
- 🌐 **Multi-protocol support**: ICMP, HTTP/HTTPS, TCP, DNS, and NTP monitoring
- 📊 **Real-time statistics**: Live success rates, response times, and packet loss
- 🖥️ **Interactive TUI**: Clean terminal interface with sortable results
- ⚡ **High performance**: Concurrent probing with configurable intervals
- 📁 **Batch mode**: Non-interactive mode for scripting and automation

## Supported Protocols

| Protocol | Format | Example | Description |
|----------|--------|---------|-------------|
| **ICMP v4** | `hostname`, `subnet`, or `icmpv4://hostname` | `8.8.8.8`, `192.168.1.0/24`, `icmpv4://google.com` | Traditional ping functionality with subnet support |
| **ICMP v6** | `icmpv6://hostname` | `icmpv6://google.com` | IPv6 ping support |
| **HTTP/HTTPS** | `http://url` or `https://url` | `https://google.com` | Web service monitoring |
| **TCP** | `tcp://host:port` | `tcp://google.com:443` | Port connectivity testing |
| **DNS** | `dns://[server[:port]]/domain[/record_type]` | `dns://8.8.8.8/google.com/A`, `dns:///google.com` | DNS query monitoring |
| **NTP** | `ntp://[server[:port]]` | `ntp://pool.ntp.org`, `ntp://time.google.com:123` | Network Time Protocol monitoring |

## Demo

![Demo](mping.gif)

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
```

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
