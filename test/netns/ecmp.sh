#!/usr/bin/env bash
# Builds a small IPv4 network with a real ECMP hop out of Linux network
# namespaces, for the ECMP path trace test (internal/trace/ecmp_integration_test.go).
#
#                          +-- mping-ra (10.101.0.2) --+
#   mping-c --- mping-r1 --+                           +-- mping-d (10.200.0.1)
#  10.100.0.2   10.100.0.1 +-- mping-rb (10.102.0.2) --+
#
# mping-r1 reaches 10.200.0.0/24 over two equal-cost next hops and hashes on
# the L4 ports (fib_multipath_hash_policy=1), like the routers in the field.
# A UDP trace from mping-c must therefore see both mping-ra and mping-rb at
# hop 2, an ICMP trace (fixed echo ID, no ports) only one of them.
#
# Usage (as root):
#   test/netns/ecmp.sh up     create the namespaces
#   test/netns/ecmp.sh down   remove them
#   then run commands inside the client: ip netns exec mping-c <cmd>
set -euo pipefail

NS=(mping-c mping-r1 mping-ra mping-rb mping-d)

down() {
	for ns in "${NS[@]}"; do
		ip netns del "$ns" 2>/dev/null || true
	done
}

# link <ns1> <if1> <addr1> <ns2> <if2> <addr2>
link() {
	ip link add "$2" netns "$1" type veth peer name "$5" netns "$4"
	ip -n "$1" addr add "$3" dev "$2"
	ip -n "$4" addr add "$6" dev "$5"
	ip -n "$1" link set "$2" up
	ip -n "$4" link set "$5" up
}

up() {
	down
	for ns in "${NS[@]}"; do
		ip netns add "$ns"
		ip -n "$ns" link set lo up
		# Routers must answer every probe: no ICMP rate limiting, no
		# reverse path filtering (the return path is asymmetric)
		ip netns exec "$ns" sysctl -qw net.ipv4.ip_forward=1 \
			net.ipv4.icmp_ratelimit=0 net.ipv4.icmp_msgs_per_sec=100000 \
			net.ipv4.conf.all.rp_filter=0 net.ipv4.conf.default.rp_filter=0
	done

	link mping-c c-r1 10.100.0.2/24 mping-r1 r1-c 10.100.0.1/24
	link mping-r1 r1-a 10.101.0.1/24 mping-ra ra-r1 10.101.0.2/24
	link mping-r1 r1-b 10.102.0.1/24 mping-rb rb-r1 10.102.0.2/24
	link mping-ra ra-d 10.103.0.1/24 mping-d d-a 10.103.0.2/24
	link mping-rb rb-d 10.104.0.1/24 mping-d d-b 10.104.0.2/24
	ip -n mping-d addr add 10.200.0.1/32 dev lo

	ip -n mping-c route add default via 10.100.0.1
	ip netns exec mping-r1 sysctl -qw net.ipv4.fib_multipath_hash_policy=1
	ip -n mping-r1 route add 10.200.0.0/24 \
		nexthop via 10.101.0.2 dev r1-a weight 1 \
		nexthop via 10.102.0.2 dev r1-b weight 1
	ip -n mping-ra route add 10.200.0.0/24 via 10.103.0.2
	ip -n mping-ra route add 10.100.0.0/24 via 10.101.0.1
	ip -n mping-rb route add 10.200.0.0/24 via 10.104.0.2
	ip -n mping-rb route add 10.100.0.0/24 via 10.102.0.1
	ip -n mping-d route add 10.100.0.0/24 via 10.103.0.1

	ip -n mping-r1 route show
	ip netns exec mping-c ping -c 1 -W 2 10.200.0.1
}

case "${1:-}" in
up) up ;;
down) down ;;
*)
	echo "usage: $0 up|down" >&2
	exit 2
	;;
esac
