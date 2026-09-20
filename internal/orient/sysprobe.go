package orient

import (
	"bufio"
	"context"
	"net"
	"os"
	"runtime"
	"strings"
)

// This file is the whole of CARTA's platform-specific surface: five functions
// that read what the operating system already knows.
//
// It shells out to the OS's own tools rather than calling platform APIs
// through cgo, for one reason that outweighs the ugliness of parsing text: a
// pure-Go binary cross-compiles to every target from one build host with
// CGO_ENABLED=0, and a tool whose entire premise is "runs on whatever machine
// is at the site" cannot afford a build matrix. The parsers are defensive
// because output formats vary by version and locale; every one of them is
// allowed to come back empty, and an empty field is rendered as unknown rather
// than as a default.

func (p *Profile) readRouting(ctx context.Context) {
	switch runtime.GOOS {
	case "windows":
		// "route print -4" is stable across every Windows since XP. The default
		// route is the row whose destination and mask are both 0.0.0.0.
		for _, line := range lines(run(ctx, "route", "print", "-4")) {
			f := fields(line)
			if len(f) >= 4 && f[0] == "0.0.0.0" && f[1] == "0.0.0.0" && net.ParseIP(f[2]) != nil {
				// Prefer the route whose interface address is ours; several
				// default routes exist when a VPN or hypervisor is present.
				if p.IP == nil || f[3] == p.IP.String() || p.Gateway == "" {
					p.Gateway = f[2]
				}
			}
		}
		p.readWindowsIPConfig(ctx)
	case "darwin":
		for _, line := range lines(run(ctx, "route", "-n", "get", "default")) {
			if f := fields(line); len(f) == 2 && f[0] == "gateway:" {
				p.Gateway = f[1]
			}
		}
	default: // linux and the BSDs
		out := run(ctx, "ip", "-4", "route", "show", "default")
		if out == "" {
			out = run(ctx, "route", "-n")
		}
		for _, line := range lines(out) {
			f := fields(line)
			// "default via 192.168.0.1 dev wlan0 ..." or a route -n row
			for i := 0; i < len(f)-1; i++ {
				if f[i] == "via" && net.ParseIP(f[i+1]) != nil {
					p.Gateway = f[i+1]
				}
			}
			if len(f) >= 3 && f[0] == "0.0.0.0" && net.ParseIP(f[1]) != nil {
				p.Gateway = f[1]
			}
		}
	}
}

// readWindowsIPConfig pulls the DHCP server, resolvers and search suffix out of
// the adapter block belonging to our interface. ipconfig indents continuation
// lines, so a "DNS Servers" entry with three servers is one labelled line and
// two bare ones.
func (p *Profile) readWindowsIPConfig(ctx context.Context) {
	out := run(ctx, "ipconfig", "/all")
	if out == "" {
		return
	}
	var inOurs bool
	var lastKey string
	for _, line := range lines(out) {
		if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			// Adapter header: "Wireless LAN adapter Wi-Fi:"
			name := strings.TrimSuffix(strings.TrimSpace(line), ":")
			if i := strings.Index(name, " adapter "); i >= 0 {
				name = name[i+len(" adapter "):]
			}
			inOurs = p.Interface != "" && strings.EqualFold(strings.TrimSpace(name), p.Interface)
			lastKey = ""
			continue
		}
		if !inOurs {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if ok {
			key = strings.TrimRight(strings.TrimSpace(key), ". ")
			val = strings.TrimSpace(val)
		} else {
			// A continuation of the previous key.
			val, key = strings.TrimSpace(line), lastKey
		}
		if val == "" {
			continue
		}
		switch key {
		case "DHCP Server":
			p.DHCPServer = val
		case "DNS Servers":
			if ip := net.ParseIP(strings.TrimSuffix(val, "(Preferred)")); ip != nil {
				p.Resolvers = appendUnique(p.Resolvers, ip.String())
			}
		case "Connection-specific DNS Suffix":
			p.Search = appendUnique(p.Search, val)
		}
		if ok {
			lastKey = key
		}
	}
}

func (p *Profile) readResolvers(ctx context.Context) {
	if len(p.Resolvers) > 0 {
		return
	}
	switch runtime.GOOS {
	case "darwin":
		for _, line := range lines(run(ctx, "scutil", "--dns")) {
			f := fields(line)
			if len(f) >= 3 && strings.HasPrefix(f[0], "nameserver[") {
				p.Resolvers = appendUnique(p.Resolvers, f[2])
			}
			if len(f) >= 3 && f[0] == "search" && f[1] == "domain[0]" {
				p.Search = appendUnique(p.Search, f[2])
			}
		}
	default:
		f, err := os.Open("/etc/resolv.conf")
		if err != nil {
			return
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fs := fields(sc.Text())
			if len(fs) < 2 || strings.HasPrefix(fs[0], "#") {
				continue
			}
			switch fs[0] {
			case "nameserver":
				p.Resolvers = appendUnique(p.Resolvers, fs[1])
			case "search", "domain":
				p.Search = appendUnique(p.Search, fs[1])
			}
		}
	}
}

// ReadNeighbours reads the ARP/NDP cache. This is pure observation: the entries
// are there because the host already talked to those addresses, so reading them
// costs nothing and emits nothing. What is in the cache before CARTA runs is
// also worth recording separately from what CARTA's own sweep puts there — a
// device the host was already talking to is a different kind of evidence from
// one that merely answered a probe.
//
// It is exported because the sweep re-reads the cache after warming it: the
// difference between the cache before and after is layer-2 evidence that no
// single read can give you.
func ReadNeighbours(ctx context.Context) []Neighbour {
	var out []Neighbour
	switch runtime.GOOS {
	case "windows":
		for _, line := range lines(run(ctx, "arp", "-a")) {
			f := fields(line)
			if len(f) >= 3 && net.ParseIP(f[0]) != nil && strings.Count(f[1], "-") == 5 {
				out = append(out, Neighbour{IP: f[0], MAC: normaliseMAC(f[1]), State: f[2]})
			}
		}
	case "darwin":
		// "? (192.168.0.1) at b8:fb:b3:95:58:d on en0 ifscope [ethernet]"
		for _, line := range lines(run(ctx, "arp", "-an")) {
			f := fields(line)
			if len(f) >= 4 && f[2] == "at" {
				ip := strings.Trim(f[1], "()")
				if net.ParseIP(ip) != nil && strings.Contains(f[3], ":") {
					out = append(out, Neighbour{IP: ip, MAC: normaliseMAC(f[3]), State: "dynamic"})
				}
			}
		}
	default:
		text := run(ctx, "ip", "neigh", "show")
		if text == "" {
			text = run(ctx, "arp", "-an")
		}
		for _, line := range lines(text) {
			f := fields(line)
			if len(f) >= 5 && net.ParseIP(f[0]) != nil {
				n := Neighbour{IP: f[0], State: strings.ToLower(f[len(f)-1])}
				for i := 0; i < len(f)-1; i++ {
					if f[i] == "lladdr" {
						n.MAC = normaliseMAC(f[i+1])
					}
				}
				if n.MAC != "" {
					out = append(out, n)
				}
			}
		}
	}
	return out
}

func (p *Profile) readWireless(ctx context.Context) {
	switch runtime.GOOS {
	case "windows":
		var ssid, bssid, band, channel, signal string
		for _, line := range lines(run(ctx, "netsh", "wlan", "show", "interfaces")) {
			key, val, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			key, val = strings.TrimSpace(key), strings.TrimSpace(val)
			switch key {
			case "SSID":
				ssid = val
			case "AP BSSID", "BSSID":
				bssid = val
			case "Band":
				band = val
			case "Channel":
				channel = val
			case "Signal":
				signal = val
			}
		}
		if ssid != "" {
			p.Wireless = ssid
			if bssid != "" {
				p.Wireless += " (" + strings.TrimSpace(strings.Join([]string{bssid, band, "ch " + channel, signal}, ", ")) + ")"
			}
		}
	case "darwin":
		const airport = "/System/Library/PrivateFrameworks/Apple80211.framework/Versions/Current/Resources/airport"
		for _, line := range lines(run(ctx, airport, "-I")) {
			if key, val, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(key) == "SSID" {
				p.Wireless = strings.TrimSpace(val)
			}
		}
	default:
		if s := strings.TrimSpace(run(ctx, "iwgetid", "-r")); s != "" {
			p.Wireless = s
		}
	}
}

// detectConfounders records the ways this host is a biased instrument.
//
// Each one is recorded rather than corrected for, because correcting would mean
// guessing. A VPN carrying the default route does not invalidate a survey of
// the local segment, but it does explain why the gateway looks strange, and the
// reader deserves to be told rather than left to work it out.
func (p *Profile) detectConfounders(ctx context.Context) {
	for _, ifc := range p.Interfaces {
		if !ifc.Up || len(ifc.Addrs) == 0 {
			continue
		}
		n := strings.ToLower(ifc.Name)
		switch {
		case strings.HasPrefix(n, "tun"), strings.HasPrefix(n, "tap"), strings.HasPrefix(n, "wg"),
			strings.HasPrefix(n, "utun"), strings.HasPrefix(n, "ppp"),
			strings.Contains(n, "vpn"), strings.Contains(n, "wireguard"), strings.Contains(n, "tailscale"),
			strings.Contains(n, "zerotier"):
			p.Confounders = append(p.Confounders,
				"VPN-like interface up: "+ifc.Name+" — results may be measured through a tunnel")
		case strings.Contains(n, "vethernet"), strings.Contains(n, "vmnet"), strings.Contains(n, "virtualbox"),
			strings.Contains(n, "docker"), strings.Contains(n, "wsl"), strings.Contains(n, "hyper-v"):
			p.Confounders = append(p.Confounders,
				"Virtual interface up: "+ifc.Name+" — its subnet is a host-local fabric, not part of this network")
		}
	}

	switch runtime.GOOS {
	case "windows":
		out := run(ctx, "netsh", "advfirewall", "show", "currentprofile", "state")
		if strings.Contains(strings.ToUpper(out), "ON") {
			p.Confounders = append(p.Confounders,
				"Host firewall is ON — inbound replies to our probes are unaffected, but this host is invisible to peers")
		}
		if px := run(ctx, "netsh", "winhttp", "show", "proxy"); strings.Contains(px, "Proxy Server(s)") {
			p.Confounders = append(p.Confounders, "A system proxy is configured — HTTP fingerprints may describe the proxy, not the device")
		}
	default:
		for _, v := range []string{"http_proxy", "HTTP_PROXY", "https_proxy", "HTTPS_PROXY"} {
			if os.Getenv(v) != "" {
				p.Confounders = append(p.Confounders, "Proxy environment variable "+v+" is set — HTTP fingerprints may describe the proxy")
				break
			}
		}
	}
}

func lines(s string) []string { return strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") }

// normaliseMAC converts every platform's spelling to lower-case colon form, so
// that an address read from a Windows ARP table and one read from an mDNS
// record are the same identity in the model.
func normaliseMAC(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "-", ":"))
	parts := strings.Split(s, ":")
	if len(parts) != 6 {
		return ""
	}
	for i, p := range parts {
		if len(p) == 1 {
			parts[i] = "0" + p
		}
	}
	return strings.Join(parts, ":")
}

func appendUnique(xs []string, v string) []string {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}
