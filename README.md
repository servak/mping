mping
=====

[![build_status](https://travis-ci.org/servak/mping.svg?branch=master)](https://travis-ci.org/servak/mping)

mping is a program to send ICMP echo.

## Demo

![mping](https://cloud.githubusercontent.com/assets/1210536/17810864/9676f0ea-665a-11e6-99dd-8166789fc0d2.gif)

## Usage

```
> mping
Usage:
  mping [options] [host ...]

Options:
  -6   	use ip v6
  -f string
       	use contents of file
  -i int
       	interval(ms) (default 1000)
  -t string
       	print title
  -v   	print version of mping
Example:
  mping localhost 8.8.8.8
  mping -f hostslist
```
