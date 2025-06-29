mping
=====

![CI](https://github.com/servak/mping/actions/workflows/ci.yml/badge.svg)

**mping** is a multi-target, multi-protocol network monitoring tool that extends traditional ping functionality. Monitor multiple hosts and services simultaneously with real-time statistics and an interactive terminal UI.

## Features

- 🎯 **Multi-target monitoring**: Monitor dozens of hosts simultaneously
- 🌐 **Multi-protocol support**: ICMP, HTTP/HTTPS, and TCP connectivity testing
- 📊 **Real-time statistics**: Live success rates, response times, and packet loss
- 🖥️ **Interactive TUI**: Clean terminal interface with sortable results
- ⚡ **High performance**: Concurrent probing with configurable intervals
- 📁 **Batch mode**: Non-interactive mode for scripting and automation

## Supported Protocols

| Protocol | Format | Example | Description |
|----------|--------|---------|-------------|
| **ICMP v4** | `hostname` or `icmpv4:hostname` | `8.8.8.8`, `icmpv4:google.com` | Traditional ping functionality |
| **ICMP v6** | `icmpv6:hostname` | `icmpv6:google.com` | IPv6 ping support |
| **HTTP/HTTPS** | `http://url` or `https://url` | `https://google.com` | Web service monitoring |
| **TCP** | `tcp://host:port` | `tcp://google.com:443` | Port connectivity testing |

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

# Mix different protocols
mping google.com https://google.com tcp://google.com:443

# Monitor web services
mping https://github.com https://google.com http://httpbin.org

# IPv6 support
mping icmpv6:google.com icmpv6:2001:4860:4860::8888
```

### Batch mode for automation
```bash
# Run 10 probes and exit
mping batch --count 10 google.com https://api.example.com

# Use with external host lists
mping batch -f hosts.txt --count 5
```

## Usage

```
Usage:
  mping [IP or HOSTNAME]... [flags]
  mping [command]

Examples:
mping 1.1.1.1 8.8.8.8
mping icmpv6:google.com
mping http://google.com
mping tcp://google.com:443 https://google.com 8.8.8.8

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
