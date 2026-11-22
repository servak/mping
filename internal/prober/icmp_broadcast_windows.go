package prober

import "golang.org/x/net/icmp"

// allowBroadcast does nothing: on Windows only raw ICMP sockets are used, on
// which the net package allows broadcasts.
func allowBroadcast(*icmp.PacketConn) {}
