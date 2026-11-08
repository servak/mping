package prober

import (
	"fmt"
	"net"
	"strings"
	"time"
)

// Helpers shared by the probers: target prefix parsing, event construction
// and error classification.

// cutPrefix strips the prober prefix from target, accepting both the
// "prefix://host" form and the legacy "prefix:host" form. It returns target
// unchanged and false when target carries neither.
func cutPrefix(target, prefix string) (string, bool) {
	if rest, ok := strings.CutPrefix(target, prefix+"://"); ok {
		return rest, true
	}
	if rest, ok := strings.CutPrefix(target, prefix+":"); ok {
		return rest, true
	}
	return target, false
}

func registerEvent(key, displayName string, probeType ProbeType) *Event {
	return &Event{Key: key, DisplayName: displayName, Result: REGISTER, ProbeType: probeType}
}

func sentEvent(key, displayName string, sentTime time.Time) *Event {
	return &Event{Key: key, DisplayName: displayName, Result: SENT, SentTime: sentTime}
}

func successEvent(key, displayName string, sentTime time.Time, rtt time.Duration, details *ProbeDetails) *Event {
	return &Event{
		Key:         key,
		DisplayName: displayName,
		Result:      SUCCESS,
		SentTime:    sentTime,
		Rtt:         rtt,
		Details:     details,
	}
}

// failureEvent builds a TIMEOUT or FAILED event.
func failureEvent(key, displayName string, result reason, sentTime time.Time, rtt time.Duration, message string) *Event {
	return &Event{
		Key:         key,
		DisplayName: displayName,
		Result:      result,
		SentTime:    sentTime,
		Rtt:         rtt,
		Message:     message,
	}
}

// errorEvent reports a probe that failed with err, as TIMEOUT or FAILED
// according to classifyErr.
func errorEvent(key, displayName string, sentTime time.Time, err error) *Event {
	return failureEvent(key, displayName, classifyErr(err), sentTime, 0, err.Error())
}

// classifyErr maps a probe error to TIMEOUT when it is a net.Error timeout
// and to FAILED otherwise. Only err itself is inspected, not the errors it
// wraps, so a timeout wrapped with extra context is still reported as FAILED.
func classifyErr(err error) reason {
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return TIMEOUT
	}
	return FAILED
}

// interfaceIP returns an address of the named interface in the wanted
// family (IPv6 when v6 is set, IPv4 otherwise), skipping loopback addresses
// when skipLoopback is set. See pickInterfaceIP for the preference.
func interfaceIP(name string, v6, skipLoopback bool) (*net.IPAddr, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, fmt.Errorf("interface not found: %s", name)
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("failed to get addresses for interface %s: %v", name, err)
	}
	return pickInterfaceIP(iface.Name, addrs, v6, skipLoopback)
}

// pickInterfaceIP picks the source address among the addresses of the
// interface name: a global unicast address if there is one, else a
// link-local one, else any other (a loopback address unless skipLoopback).
// IPv6 link-local addresses carry the interface as zone, without which
// they cannot be bound.
func pickInterfaceIP(name string, addrs []net.Addr, v6, skipLoopback bool) (*net.IPAddr, error) {
	var linkLocal, other *net.IPAddr
	for _, addr := range addrs {
		ipNet, ok := addr.(*net.IPNet)
		if !ok {
			continue
		}
		ip := ipNet.IP
		if (ip.To4() == nil) != v6 {
			continue
		}
		switch {
		case skipLoopback && ip.IsLoopback():
		case ip.IsGlobalUnicast():
			return &net.IPAddr{IP: ip}, nil
		case ip.IsLinkLocalUnicast():
			if linkLocal == nil {
				linkLocal = &net.IPAddr{IP: ip}
				if v6 {
					linkLocal.Zone = name
				}
			}
		default:
			if other == nil {
				other = &net.IPAddr{IP: ip}
			}
		}
	}
	if linkLocal != nil {
		return linkLocal, nil
	}
	if other != nil {
		return other, nil
	}
	family := "IPv4"
	if v6 {
		family = "IPv6"
	}
	return nil, fmt.Errorf("no suitable %s address found on interface %s", family, name)
}
