mping
=====

![go workflow](https://github.com/servak/mping/actions/workflows/go.yml/badge.svg)

mping is a program to send ICMP echo.

## Demo

![Demo](mping.gif)

## Install

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
Usage: mping [OPTIONS] [TARGET...]
Options:
  -c, --config string      config path (default "~/.mping.yml")
  -f, --fiilename string   use contents of file
  -h, --help               Display help and exit
  -i, --interval int       interval(ms) (default 1000)
  -t, --timeout int        timeout(ms) (default 1000)
  -n, --title string       print title
  -v, --version            print version
Examples:
  mping localhost google.com 8.8.8.8 192.168.1.0/24
  mping google.com icmpv6:google.com
  mping -f hostslist
```
