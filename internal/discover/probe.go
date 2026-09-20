package discover

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Tanmay-Anand/carta/internal/model"
)

// Service identification, and the honest limits of it.
//
// An open port is evidence about a device, but weak evidence: port 9100 usually
// means a printer and occasionally means something else entirely. A banner is
// weaker still, because it is self-reported and trivially changed. A TLS
// certificate is stronger — the device had to hold the private key — and an
// HTTP title is somewhere in between.
//
// So every claim from this file carries the confidence its method deserves,
// and none of them reach Asserted except the certificate subject, which the
// device did in fact assert cryptographically.

// portHints maps a port to what it suggests. "suggests" is the operative word:
// these produce Low-confidence class claims that an mDNS or UPnP announcement
// will override.
var portHints = map[int]struct{ service, class string }{
	22:    {"ssh", ""},
	21:    {"ftp", ""},
	23:    {"telnet", ""},
	53:    {"dns", "resolver"},
	80:    {"http", ""},
	111:   {"rpcbind", ""},
	139:   {"netbios-ssn", "Windows or SMB host"},
	443:   {"https", ""},
	445:   {"smb", "Windows or SMB host"},
	502:   {"modbus", "industrial controller"},
	515:   {"lpd", "printer"},
	548:   {"afp", "file server"},
	554:   {"rtsp", "camera"},
	631:   {"ipp", "printer"},
	1883:  {"mqtt", "IoT broker"},
	1900:  {"upnp", ""},
	2049:  {"nfs", "file server"},
	3306:  {"mysql", "database server"},
	3389:  {"rdp", "Windows host"},
	5000:  {"http-alt", ""},
	5001:  {"http-alt", ""},
	5432:  {"postgresql", "database server"},
	5900:  {"vnc", "remote desktop host"},
	6379:  {"redis", "database server"},
	8006:  {"proxmox", "hypervisor"},
	8080:  {"http-alt", ""},
	8096:  {"jellyfin", "media server"},
	8123:  {"home-assistant", "smart-home controller"},
	8443:  {"https-alt", ""},
	8554:  {"rtsp-alt", "camera"},
	9000:  {"http-alt", ""},
	9100:  {"jetdirect", "printer"},
	27017: {"mongodb", "database server"},
	32400: {"plex", "media server"},
	37777: {"dahua-dvr", "camera / NVR"},
}

// Probe walks the risk-ranked port list on each responsive address and records
// what it finds, including what it looked for and did not find.
func Probe(ctx context.Context, g *model.Graph, targets []string, opt Options) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, opt.Concurrency)
	var done int
	var mu sync.Mutex

	for _, ip := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			probeHost(ctx, g, ip, opt)
			mu.Lock()
			done++
			if opt.Progress != nil {
				opt.Progress(done, len(targets), "probe")
			}
			mu.Unlock()
		}(ip)
	}
	wg.Wait()
}

func probeHost(ctx context.Context, g *model.Graph, ip string, opt Options) {
	ids := []model.Identity{model.IPIdentity(ip)}
	var openPorts []int

	for _, port := range opt.Ports {
		select {
		case <-ctx.Done():
			return
		default:
		}
		d := net.Dialer{Timeout: opt.Timeout}
		conn, err := d.DialContext(ctx, "tcp4", net.JoinHostPort(ip, itoa(port)))
		if err != nil {
			continue
		}
		openPorts = append(openPorts, port)
		now := time.Now()
		hint := portHints[port]

		g.Observe(ids, model.Claim{Key: "port", Value: itoa(port) + "/tcp", Confidence: model.High,
			Source: model.Source{Method: "tcp-connect", Evidence: "handshake completed", At: now}})
		if hint.service != "" {
			g.Observe(ids, model.Claim{Key: "service", Value: hint.service + " (" + itoa(port) + "/tcp)", Confidence: model.Moderate,
				Source: model.Source{Method: "tcp-connect", Evidence: "port " + itoa(port) + " is the registered port for " + hint.service, At: now}})
		}
		if hint.class != "" {
			g.Observe(ids, model.Claim{Key: "class", Value: hint.class, Confidence: model.Low,
				Source: model.Source{Method: "tcp-connect", Evidence: "inferred from an open port, which is weak evidence", At: now}})
		}

		banner := readBanner(conn, opt.Timeout)
		_ = conn.Close()
		if banner != "" {
			g.Observe(ids, model.Claim{Key: "banner", Value: trim(banner, 120), Confidence: model.Low,
				Source: model.Source{Method: "banner", Evidence: "self-reported on connect to " + itoa(port), At: now}})
			if v := vendorFromBanner(banner); v != "" {
				g.Observe(ids, model.Claim{Key: "vendor", Value: v, Confidence: model.Low,
					Source: model.Source{Method: "banner", Evidence: trim(banner, 80), At: now}})
			}
			if os := osFromBanner(banner); os != "" {
				g.Observe(ids, model.Claim{Key: "os", Value: os, Confidence: model.Low,
					Source: model.Source{Method: "banner", Evidence: trim(banner, 80), At: now}})
			}
		}

		switch port {
		case 443, 8443, 9443:
			inspectTLS(ctx, g, ids, ip, port, opt.Timeout)
			fingerprintHTTP(ctx, g, ids, "https://"+net.JoinHostPort(ip, itoa(port)), opt.Timeout)
		case 80, 8080, 8000, 8006, 8096, 8123, 5000, 5001, 9000, 32400:
			fingerprintHTTP(ctx, g, ids, "http://"+net.JoinHostPort(ip, itoa(port)), opt.Timeout)
		}
	}

	if len(openPorts) == 0 {
		// Recorded deliberately: "probed, nothing open" is a different statement
		// from "not probed", and only one of them belongs in a report as an
		// empty services column.
		g.Observe(ids, model.Claim{Key: "probed", Value: "no open ports in the risk-ranked set", Confidence: model.High,
			Source: model.Source{Method: "tcp-connect", Evidence: itoa(len(opt.Ports)) + " ports probed, none accepted a connection", At: time.Now()}})
	}
}

// readBanner reads whatever a service volunteers on connect. Services that wait
// for the client to speak first (HTTP, most of them) simply time out here,
// which costs one timeout and is why the budget is short.
func readBanner(conn net.Conn, timeout time.Duration) string {
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil || n == 0 {
		return ""
	}
	return sanitise(string(buf[:n]))
}

// inspectTLS reads the certificate a device serves. The subject and SANs are
// the device's own cryptographic assertion of its identity, which makes them
// the strongest name evidence available at this tier — even on the
// self-signed certificates that fill a private network.
func inspectTLS(ctx context.Context, g *model.Graph, ids []model.Identity, ip string, port int, timeout time.Duration) {
	d := &net.Dialer{Timeout: timeout}
	// InsecureSkipVerify is correct here and nowhere else: we are cataloguing
	// certificates, not trusting them, and a survey that refused to look at
	// self-signed certificates would be blind to most of a private network.
	conn, err := tls.DialWithDialer(d, "tcp4", net.JoinHostPort(ip, itoa(port)), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.Close()
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return
	}
	cert := state.PeerCertificates[0]
	now := time.Now()

	if cn := cert.Subject.CommonName; cn != "" && !strings.EqualFold(cn, ip) {
		g.Observe(ids, model.Claim{Key: "hostname", Value: cn, Confidence: model.Asserted,
			Source: model.Source{Method: "tls-cert", Evidence: "certificate subject CN on " + itoa(port), At: now}})
	}
	if org := strings.Join(cert.Subject.Organization, ", "); org != "" {
		g.Observe(ids, model.Claim{Key: "vendor", Value: org, Confidence: model.Moderate,
			Source: model.Source{Method: "tls-cert", Evidence: "certificate subject O", At: now}})
	}
	for _, san := range cert.DNSNames {
		if san != "" && !strings.HasPrefix(san, "*") {
			g.Observe(append(ids, model.HostIdentity(san)),
				model.Claim{Key: "hostname", Value: san, Confidence: model.Asserted,
					Source: model.Source{Method: "tls-cert", Evidence: "certificate SAN", At: now}})
		}
	}
	if time.Now().After(cert.NotAfter) {
		g.Observe(ids, model.Claim{Key: "note", Value: "TLS certificate expired " + cert.NotAfter.Format("2006-01-02"), Confidence: model.High,
			Source: model.Source{Method: "tls-cert", Evidence: "notAfter in the past", At: now}})
	}
}

var titleRE = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// fingerprintHTTP identifies management interfaces, which is where most of a
// network's identity actually lives: routers, NAS boxes, printers, cameras and
// hypervisors all present a web UI whose title and headers name the product.
func fingerprintHTTP(ctx context.Context, g *model.Graph, ids []model.Identity, rawurl string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(ctx, timeout*4)
	defer cancel()

	client := &http.Client{
		Timeout: timeout * 4,
		Transport: &http.Transport{
			TLSClientConfig:   &tls.Config{InsecureSkipVerify: true},
			DisableKeepAlives: true,
		},
		// Redirects are not followed: a device that redirects to its vendor's
		// cloud portal would otherwise have the portal's title recorded as its
		// own identity.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", "carta/0.1 (network survey)")
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	now := time.Now()
	if server := resp.Header.Get("Server"); server != "" {
		g.Observe(ids, model.Claim{Key: "service", Value: server + " (HTTP)", Confidence: model.Moderate,
			Source: model.Source{Method: "http", Evidence: "Server header at " + rawurl, At: now}})
		if v := vendorFromBanner(server); v != "" {
			g.Observe(ids, model.Claim{Key: "vendor", Value: v, Confidence: model.Low,
				Source: model.Source{Method: "http", Evidence: "Server: " + server, At: now}})
		}
	}
	if realm := resp.Header.Get("WWW-Authenticate"); realm != "" {
		g.Observe(ids, model.Claim{Key: "note", Value: "HTTP authentication required: " + trim(realm, 60), Confidence: model.High,
			Source: model.Source{Method: "http", Evidence: rawurl, At: now}})
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 128<<10))
	if err != nil {
		return
	}
	if m := titleRE.FindSubmatch(body); len(m) == 2 {
		title := sanitise(strings.TrimSpace(string(m[1])))
		if title != "" {
			g.Observe(ids, model.Claim{Key: "http-title", Value: trim(title, 80), Confidence: model.Moderate,
				Source: model.Source{Method: "http", Evidence: "page title at " + rawurl, At: now}})
			if class := classFromTitle(title); class != "" {
				g.Observe(ids, model.Claim{Key: "class", Value: class, Confidence: model.Moderate,
					Source: model.Source{Method: "http", Evidence: "management interface title: " + trim(title, 60), At: now}})
			}
			if v := vendorFromBanner(title); v != "" {
				g.Observe(ids, model.Claim{Key: "vendor", Value: v, Confidence: model.Moderate,
					Source: model.Source{Method: "http", Evidence: "page title", At: now}})
			}
		}
	}
}

// vendorFromBanner matches product and vendor names that appear verbatim in
// banners, Server headers and page titles. The list is short and explicit
// rather than clever: a fuzzy matcher here would produce confident nonsense,
// and a name CARTA does not know is better left unclaimed.
var vendorNames = []string{
	"MikroTik", "Ubiquiti", "UniFi", "TP-Link", "Netgear", "D-Link", "ASUS", "Linksys", "Tenda",
	"Cisco", "Aruba", "Juniper", "Huawei", "ZTE", "Zyxel", "Fortinet", "pfSense", "OPNsense",
	"Synology", "QNAP", "TrueNAS", "FreeNAS", "OpenWrt", "DD-WRT", "Proxmox", "VMware", "Dahua",
	"Hikvision", "Axis", "Reolink", "Amcrest", "HP", "Hewlett-Packard", "Canon", "Epson", "Brother",
	"Xerox", "Lexmark", "Ricoh", "Kyocera", "Samsung", "Sony", "LG", "Philips", "Sonos", "Roku",
	"Raspbian", "Raspberry", "Plex", "Jellyfin", "Emby", "Home Assistant",
}

// Web servers, SSH daemons and frameworks are deliberately absent from that
// list. "Apache" is not the manufacturer of the laptop it runs on, and a
// survey that files software under vendor produces an inventory where half the
// hardware is made by nginx.

func vendorFromBanner(s string) string {
	for _, v := range vendorNames {
		if strings.Contains(strings.ToLower(s), strings.ToLower(v)) {
			return v
		}
	}
	return ""
}

func osFromBanner(s string) string {
	l := strings.ToLower(s)
	switch {
	case strings.Contains(l, "ubuntu"):
		return "Linux (Ubuntu)"
	case strings.Contains(l, "debian"):
		return "Linux (Debian)"
	case strings.Contains(l, "raspbian"):
		return "Linux (Raspberry Pi OS)"
	case strings.Contains(l, "centos"), strings.Contains(l, "red hat"):
		return "Linux (RHEL family)"
	case strings.Contains(l, "freebsd"):
		return "FreeBSD"
	case strings.Contains(l, "mikrotik"), strings.Contains(l, "routeros"):
		return "RouterOS"
	case strings.Contains(l, "openwrt"):
		return "OpenWrt"
	case strings.Contains(l, "windows"), strings.Contains(l, "microsoft-iis"):
		return "Windows"
	}
	return ""
}

func classFromTitle(title string) string {
	l := strings.ToLower(title)
	switch {
	case strings.Contains(l, "router"), strings.Contains(l, "gateway"), strings.Contains(l, "broadband"):
		return "router / gateway"
	case strings.Contains(l, "camera"), strings.Contains(l, "nvr"), strings.Contains(l, "dvr"):
		return "camera / NVR"
	case strings.Contains(l, "printer"), strings.Contains(l, "laserjet"), strings.Contains(l, "officejet"):
		return "printer"
	case strings.Contains(l, "nas"), strings.Contains(l, "diskstation"), strings.Contains(l, "synology"), strings.Contains(l, "qnap"):
		return "NAS"
	case strings.Contains(l, "proxmox"), strings.Contains(l, "esxi"), strings.Contains(l, "vsphere"):
		return "hypervisor"
	case strings.Contains(l, "home assistant"):
		return "smart-home controller"
	case strings.Contains(l, "plex"), strings.Contains(l, "jellyfin"), strings.Contains(l, "emby"):
		return "media server"
	case strings.Contains(l, "switch"), strings.Contains(l, "access point"):
		return "network equipment"
	}
	return ""
}

// sanitise strips control characters so a hostile banner cannot inject ANSI
// escapes into a terminal or line breaks into a Markdown table.
func sanitise(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n', r == '\r', r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r > 0x7e:
			// Dropped. A banner is bytes from an unknown device: SSH servers
			// follow their version string with binary key exchange, and a
			// printer may answer in any encoding. Anything outside printable
			// ASCII is not text we can attribute meaning to.
		case r == '|':
			b.WriteString(`\|`)
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(strings.Join(strings.Fields(b.String()), " "))
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
