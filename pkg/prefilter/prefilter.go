// Package prefilter provides a cheap TCP liveness pre-check that runs before
// the (expensive) headless-Chrome navigation. On large recon lists a big
// fraction of targets are dead (NXDOMAIN / refused / hang-to-timeout) and each
// one otherwise burns a full Chrome navigation timeout (default 60s). A fast
// net.DialTimeout culls them in ~dial-timeout seconds on a cheap goroutine.
package prefilter

import (
	"net"
	"net/url"
	"time"
)

// IsLive returns true if a TCP connection to the target's host:port can be
// established within timeout. It does NOT validate TLS or HTTP — the goal is
// only to drop hosts that cannot accept a connection at all, so we never
// false-negative a host that has an open port. DNS failures (NXDOMAIN),
// connection-refused and dial timeouts all return false quickly.
func IsLive(target string, timeout time.Duration) bool {
	u, err := url.Parse(target)
	if err != nil {
		// can't parse — let the downstream witness path deal with it
		return true
	}

	host := u.Hostname()
	if host == "" {
		return true
	}

	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
