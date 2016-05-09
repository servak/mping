mping
=====

[![build_status](https://travis-ci.org/servak/mping.svg?branch=master)](https://travis-ci.org/servak/mping)

mping is a program to send ICMP echo.

## Demo

![gif](https://cloud.githubusercontent.com/assets/1210536/15098387/37db406a-1577-11e6-8b49-7f2dbab5b29a.gif)

## Usage

```
> ./mping
Usage:
  ./mping [options] [host ...]

Options:
  -f string
        use contents of file (shorthand)
  -file string
        use contents of file
Example:
  ./mping localhost 8.8.8.8
  ./mping -f hostslist```
