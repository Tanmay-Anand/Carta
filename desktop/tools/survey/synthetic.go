package survey

import (
	"time"

	"github.com/Tanmay-Anand/carta/internal/model"
)

// Synthetic builds a survey of a fictional 192.168.10.0/24, the same network
// the README's screenshots show. Its hardware addresses come from the range
// RFC 7042 reserves for documentation (00:00:5e:00:53:xx): globally
// administered, so drift can match on them, and owned by no real device. It goes through Graph.Observe, the path real
// discovery uses, so merges and identities come out the way a survey's would.
//
// It exists for the UI mock and for tests. No real network belongs in a
// screenshot, a test fixture or this repository.
func Synthetic() *model.Graph {
	return synthetic("20261123T102600Z", time.Date(2026, 11, 23, 10, 26, 0, 0, time.UTC), true)
}

// SyntheticEarlier is the same network a week before: one phone not yet
// joined, the camera on another address, the NAS without its newer service.
// Comparing the two gives the mock a Drift tab with real drift.Compare output.
func SyntheticEarlier() *model.Graph {
	return synthetic("20261116T091400Z", time.Date(2026, 11, 16, 9, 14, 0, 0, time.UTC), false)
}

func synthetic(id string, at time.Time, later bool) *model.Graph {
	g := model.NewGraph(id)
	g.StartedAt = at
	g.Vantage = model.Vantage{
		Host: "carta-laptop", Interface: "Wi-Fi", IP: "192.168.10.20", MAC: "00:00:5e:00:53:14",
		Prefix: "192.168.10.0/24", Gateway: "192.168.10.1", Resolvers: []string{"192.168.10.1"},
		Wireless: "lab-example", OS: "windows", DHCPServer: "192.168.10.1", Instrument: "carta-desktop",
	}

	src := func(method, evidence string) model.Source {
		return model.Source{Method: method, Evidence: evidence, At: at}
	}
	claim := func(key, value string, c model.Confidence, method, evidence string) model.Claim {
		return model.Claim{Key: key, Value: value, Confidence: c, Source: src(method, evidence)}
	}
	alive := func(how string) model.Claim {
		return claim("alive", "yes", model.High, "tcp-connect", how)
	}
	ip, mac, host := model.IPIdentity, model.MACIdentity, model.HostIdentity

	gw := g.Observe([]model.Identity{ip("192.168.10.1"), mac("00:00:5e:00:53:01")},
		alive("443/tcp handshake"),
		claim("class", "router / gateway", model.High, "route-table", "default via 192.168.10.1 dev Wi-Fi"),
		claim("vendor", "Northwind Networks", model.Moderate, "oui", "00:00:5E"),
		claim("service", "https (443/tcp)", model.High, "tcp-connect", "443/tcp open"),
		claim("service", "dns (53/udp)", model.High, "rdns", "answered PTR queries"),
		claim("model", "NW-AX3000", model.Asserted, "ssdp", "modelName: NW-AX3000"),
		claim("os", "Linux", model.Low, "http", "Server: lighttpd"))
	g.Observe([]model.Identity{ip("192.168.10.1"), host("gateway.lab.example")},
		claim("hostname", "gateway.lab.example", model.Moderate, "rdns", "1.10.168.192.in-addr.arpa PTR gateway.lab.example."))

	self := g.Observe([]model.Identity{ip("192.168.10.20"), mac("00:00:5e:00:53:14"), host("carta-laptop")},
		alive("this machine"),
		claim("hostname", "carta-laptop", model.Asserted, "host", "os.Hostname"),
		claim("class", "survey vantage", model.High, "host", "the instrument itself"),
		claim("os", "Windows", model.Asserted, "host", "runtime.GOOS"))

	ap := g.Observe([]model.Identity{ip("192.168.10.2"), mac("00:00:5e:00:53:02"), host("ap-upstairs.lab.example")},
		alive("80/tcp handshake"),
		claim("hostname", "ap-upstairs.lab.example", model.Moderate, "rdns", "PTR ap-upstairs.lab.example."),
		claim("class", "access point", model.Moderate, "ssdp", "deviceType: WLANAccessPointDevice"),
		claim("vendor", "Northwind Networks", model.Moderate, "oui", "00:00:5E"))

	nasClaims := []model.Claim{
		alive("445/tcp handshake"),
		claim("hostname", "nas.local", model.Asserted, "mdns", "nas.local A 192.168.10.5"),
		claim("class", "storage / NAS", model.Asserted, "mdns", "_smb._tcp, _afpovertcp._tcp"),
		claim("vendor", "Contoso Storage", model.Moderate, "oui", "00:00:5E"),
		claim("service", "smb (445/tcp)", model.Asserted, "mdns", "_smb._tcp.local"),
	}
	if later {
		nasClaims = append(nasClaims, claim("service", "https (5001/tcp)", model.High, "tcp-connect", "5001/tcp open"))
	}
	g.Observe([]model.Identity{ip("192.168.10.5"), mac("00:00:5e:00:53:05"), host("nas.local")}, nasClaims...)

	g.Observe([]model.Identity{ip("192.168.10.12"), mac("00:00:5e:00:53:0c"), host("office-printer.local")},
		alive("631/tcp handshake"),
		claim("hostname", "office-printer.local", model.Asserted, "mdns", "office-printer.local A 192.168.10.12"),
		claim("class", "printer", model.Asserted, "mdns", "_ipp._tcp.local"),
		claim("model", "Contoso P2100", model.Asserted, "mdns", "ty=Contoso P2100"),
		claim("service", "ipp (631/tcp)", model.Asserted, "mdns", "_ipp._tcp.local"))

	g.Observe([]model.Identity{ip("192.168.10.21"), mac("00:00:5e:00:53:15"), host("studio-pc")},
		alive("445/tcp handshake"),
		claim("hostname", "studio-pc", model.Asserted, "nbns", "STUDIO-PC <00> UNIQUE"),
		claim("class", "workstation", model.Moderate, "nbns", "file server service <20>"),
		claim("os", "Windows", model.Low, "tcp-connect", "TTL 128"))

	g.Observe([]model.Identity{ip("192.168.10.30"), mac("00:00:5e:00:53:1e")},
		alive("8008/tcp handshake"),
		claim("label", "Living Room TV", model.Asserted, "ssdp", "friendlyName: Living Room TV"),
		claim("class", "media / TV", model.Asserted, "ssdp", "deviceType: MediaRenderer"))

	g.Observe([]model.Identity{ip("192.168.10.31"), mac("00:00:5e:00:53:1f"), host("kitchen-speaker.local")},
		alive("8009/tcp handshake"),
		claim("hostname", "kitchen-speaker.local", model.Asserted, "mdns", "kitchen-speaker.local A 192.168.10.31"),
		claim("class", "speaker", model.Asserted, "mdns", "_googlecast._tcp"))

	g.Observe([]model.Identity{ip("192.168.10.41"), mac("7e:5d:c3:00:00:aa")},
		alive("ARP reply"),
		claim("class", "personal device (randomised MAC)", model.Low, "oui", "locally administered bit set"))

	if later {
		g.Observe([]model.Identity{ip("192.168.10.42"), mac("3a:91:0c:00:00:bb")},
			alive("ARP reply"),
			claim("class", "personal device (randomised MAC)", model.Low, "oui", "locally administered bit set"))
	}

	g.Observe([]model.Identity{ip("192.168.10.44"), mac("00:00:5e:00:53:2c"), host("kids-tablet.local")},
		alive("ARP reply"),
		claim("hostname", "kids-tablet.local", model.Asserted, "mdns", "kids-tablet.local A 192.168.10.44"),
		claim("class", "tablet", model.Low, "mdns", "_companion-link._tcp"))

	g.Observe([]model.Identity{ip("192.168.10.50"), mac("00:00:5e:00:53:32")},
		alive("80/tcp handshake"),
		claim("vendor", "Fabrikam IoT", model.Moderate, "oui", "00:00:5E"),
		claim("class", "smart plug", model.Moderate, "http", "<title>Plug setup</title>"))

	camIP := "192.168.10.60"
	if !later {
		camIP = "192.168.10.61"
	}
	g.Observe([]model.Identity{ip(camIP), mac("00:00:5e:00:53:3c")},
		alive("554/tcp handshake"),
		claim("class", "camera", model.Low, "tcp-connect", "554/tcp (rtsp) open"),
		claim("vendor", "Fabrikam IoT", model.Moderate, "oui", "00:00:5E"),
		claim("service", "rtsp (554/tcp)", model.High, "tcp-connect", "554/tcp open"))

	g.Observe([]model.Identity{ip("192.168.10.77")}, alive("22/tcp handshake"),
		claim("service", "ssh (22/tcp)", model.High, "tcp-connect", "22/tcp open"))

	for _, e := range g.Entities {
		if e == gw {
			continue
		}
		g.AddEdge(model.Edge{From: gw.ID, To: e.ID, Kind: model.EdgeGateway, Confidence: model.Moderate,
			Source: src("route-table", "on-link, default via 192.168.10.1")})
	}
	g.AddEdge(model.Edge{From: self.ID, To: ap.ID, Kind: model.EdgeSameL2, Confidence: model.High,
		Source: src("arp-cache", "both in this host's ARP cache")})

	responded := len(g.Entities)
	g.Coverage = []model.Coverage{{
		Prefix: "192.168.10.0/24", AddrsSwept: 254, AddrsTotal: 254, Responded: responded,
		PortsProbed: []int{22, 53, 80, 443, 445, 554, 631, 8008, 8009},
		Protocols:   []string{"mdns", "ssdp", "nbns"},
		StartedAt:   at, EndedAt: at.Add(41 * time.Second),
	}}
	g.Findings = []model.Finding{
		{Severity: "risk", Title: "A camera streams without a name or an owner",
			Detail:     "192.168.10.60 answers on RTSP (554/tcp) and announces nothing about itself. Find out whose it is.",
			Confidence: model.High, Entities: []string{"ip:" + camIP}},
		{Severity: "warn", Title: "2 devices hide their hardware address",
			Detail:     "Randomised MACs are normal on phones, but they cannot be told apart across surveys.",
			Confidence: model.Moderate},
		{Severity: "info", Title: "One resolver, and it is the gateway",
			Detail:     "If 192.168.10.1 stops answering DNS, every device on this network loses names.",
			Confidence: model.High},
	}
	g.Sort()
	return g
}
