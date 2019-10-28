mping
=====

[![build_status](https://travis-ci.org/servak/mping.svg?branch=master)](https://travis-ci.org/servak/mping)

mping is a program to send ICMP echo.

## Demo

[![asciicast](https://asciinema.org/a/a969qrzhs7gi11yv74gzecrl8.png)](https://asciinema.org/a/a969qrzhs7gi11yv74gzecrl8)

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
> mping
Usage:
  mping [options] [host ...]

Options:
  -6   	use ip v6
  -c int
       	stop after receiving <count> response packets
  -f string
       	use contents of file
  -i int
       	interval(ms) (default 1000)
  -q   	quiet mode
  -t string
       	print title
  -v   	print version of mping
Example:
  mping localhost 10.1.1.0/30 8.8.8.8
  mping -f hostslist
```
