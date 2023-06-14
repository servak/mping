mping
=====

![go workflow](https://github.com/servak/mping/actions/workflows/go.yml/badge.svg)

mping is a program to send ICMP echo.

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

## Usage

```
Usage:
  mping [IP or HOSTNAME]... [flags]
  mping [command]

Examples:
mping 1.1.1.1 8.8.8.8
mping icmpv6:google.com
mping http://google.com

Available Commands:
  batch       Disables TUI and performs probing for a set number of iterations
  help        Help about any command

Flags:
  -c, --config string     config path (default "~/.mping.yml")
  -f, --filename string   use contents of file
  -h, --help              help for mping
  -i, --interval int      interval(ms) (default 1000)
  -t, --timeout int       timeout(ms) (default 1000)
  -n, --title string      print title
  -v, --version           Display version

Use "mping [command] --help" for more information about a command.
```
