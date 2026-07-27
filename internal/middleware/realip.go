package middleware

import (
	"net"
	"net/http"
	"strconv"
	"strings"
)

// TrustedProxyIP replaces chi's RealIP, which is deprecated as spoofable
// (GHSA-3fxj-6jh8-hvhx): it rewrites RemoteAddr from X-Forwarded-For,
// True-Client-IP, or X-Real-IP whether or not any proxy actually sets them.
// Since clientIP feeds the rate limiters, that let anyone reset their own
// login-attempt bucket by inventing a header.
//
// Here the headers are honoured only when the immediate peer is itself a
// trusted proxy. With no trusted proxies configured — a direct-to-internet or
// LAN deployment, which is how this ships — the headers are ignored entirely
// and RemoteAddr stands.
//
// trustedCIDRs comes from TRUSTED_PROXY_CIDRS. Set it to your load balancer's
// range when you put one in front; leaving it empty is the safe default, not
// an oversight.
func TrustedProxyIP(trustedCIDRs []string) func(http.Handler) http.Handler {
	nets := parseCIDRs(trustedCIDRs)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(nets) == 0 {
				next.ServeHTTP(w, r)
				return
			}
			peer := net.ParseIP(hostOnly(r.RemoteAddr))
			if peer == nil || !ipInAny(peer, nets) {
				// The connection did not come from a proxy we trust, so
				// whatever forwarding headers it carries are the client's own
				// invention. Leave RemoteAddr alone.
				next.ServeHTTP(w, r)
				return
			}
			if ip := forwardedClientIP(r, nets); ip != "" {
				r.RemoteAddr = net.JoinHostPort(ip, "0")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// forwardedClientIP walks X-Forwarded-For right-to-left and returns the first
// address that is not itself a trusted proxy — the real client as seen by the
// outermost hop we control. Reading left-to-right (what chi's RealIP does) is
// what makes header spoofing work, since the client controls the left end.
func forwardedClientIP(r *http.Request, nets []*net.IPNet) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		// Single-value headers carry no chain to walk, so they are only
		// trustworthy because we already know the peer is a trusted proxy.
		for _, h := range []string{"True-Client-IP", "X-Real-IP"} {
			if v := strings.TrimSpace(r.Header.Get(h)); v != "" && net.ParseIP(v) != nil {
				return v
			}
		}
		return ""
	}

	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(parts[i])
		ip := net.ParseIP(candidate)
		if ip == nil {
			continue
		}
		if ipInAny(ip, nets) {
			continue // another one of our own hops
		}
		return candidate
	}
	return ""
}

func parseCIDRs(in []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(in))
	for _, raw := range in {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		// Accept a bare address as a /32 or /128 so operators don't have to
		// remember the suffix for a single proxy.
		if !strings.Contains(raw, "/") {
			if ip := net.ParseIP(raw); ip != nil {
				bits := 32
				if ip.To4() == nil {
					bits = 128
				}
				raw = raw + "/" + strconv.Itoa(bits)
			}
		}
		if _, n, err := net.ParseCIDR(raw); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func ipInAny(ip net.IP, nets []*net.IPNet) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

func hostOnly(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}
