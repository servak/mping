mping
=====

[![build_status](https://travis-ci.org/servak/mping.svg?branch=master)](https://travis-ci.org/servak/mping)

mping is a program to send ICMP echo.

## Demo

![mping](https://cloud.githubusercontent.com/assets/1210536/17332326/5461ba4a-5909-11e6-9293-d02bb5007bdc.gif)

## Usage

```
> mping -h
Usage:
  mping [options] [host ...]

Options:
  -f string
        use contents of file (shorthand)
  -t int
        max rtt of ping. (ms) (default 1000)
  -title string
        print title
  -v6
        use ip v6
  -version
        print version
Example:
  mping localhost 8.8.8.8
  mping -f hostslist
```
