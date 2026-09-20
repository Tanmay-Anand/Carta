package discover

import (
	"bufio"
	"context"
	"encoding/xml"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Tanmay-Anand/carta/internal/model"
)

// SSDP (the discovery half of UPnP) is the other side of the multicast coin.
// Where mDNS is dominated by Apple, Linux and IoT gear, SSDP is where routers,
// smart TVs, media servers, printers and games consoles announce themselves —
// and its device description document is the single richest identity source
// available without credentials: manufacturer, model, model number, serial, and
// a friendly name, in one HTTP GET.

// SSDP performs an M-SEARCH and follows each responder's LOCATION URL.
func SSDP(ctx context.Context, g *model.Graph, window time.Duration) []string {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil
	}
	defer conn.Close()

	group := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}
	// MX is the maximum number of seconds a responder may wait before replying;
	// it exists so a hundred devices do not answer in the same millisecond.
	// Asking for 2 and listening for longer is the polite pattern.
	search := "M-SEARCH * HTTP/1.1\r\n" +
		"HOST: 239.255.255.250:1900\r\n" +
		"MAN: \"ssdp:discover\"\r\n" +
		"MX: 2\r\n" +
		"ST: ssdp:all\r\n" +
		"USER-AGENT: carta/0.1 UPnP/1.1\r\n\r\n"

	// Sent twice: UDP multicast is lossy and a missed M-SEARCH means a device
	// silently absent from the map.
	for i := 0; i < 2; i++ {
		_, _ = conn.WriteToUDP([]byte(search), group)
		time.Sleep(120 * time.Millisecond)
	}

	deadline := time.Now().Add(window)
	_ = conn.SetReadDeadline(deadline)
	buf := make([]byte, 8192)
	locations := map[string]string{} // location URL -> responder IP
	var protocols []string

	for time.Now().Before(deadline) {
		n, src, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}
		protocols = appendOnce(protocols, "ssdp")
		headers := parseSSDP(string(buf[:n]))
		ip := src.IP.String()
		now := time.Now()
		ids := []model.Identity{model.IPIdentity(ip)}

		if server := headers["server"]; server != "" {
			// "Linux/3.14 UPnP/1.0 MiniDLNA/1.2.1" — the middle of a SERVER
			// header is usually the OS, the end is the application.
			g.Observe(ids, model.Claim{Key: "os", Value: firstToken(server), Confidence: model.Moderate,
				Source: model.Source{Method: "ssdp", Evidence: "SERVER: " + server, At: now}})
			g.Observe(ids, model.Claim{Key: "service", Value: lastToken(server) + " (UPnP)", Confidence: model.Asserted,
				Source: model.Source{Method: "ssdp", Evidence: "SERVER: " + server, At: now}})
		}
		if st := headers["st"]; st != "" {
			if class := upnpClass(st); class != "" {
				g.Observe(ids, model.Claim{Key: "class", Value: class, Confidence: model.Asserted,
					Source: model.Source{Method: "ssdp", Evidence: "ST: " + st, At: now}})
			}
		}
		if loc := headers["location"]; loc != "" {
			locations[loc] = ip
		}
	}

	// Fetch the description documents after the listening window, not during:
	// an HTTP GET inside the read loop delays the socket and loses replies.
	for loc, ip := range locations {
		fetchDescription(ctx, g, loc, ip)
	}
	return protocols
}

func parseSSDP(raw string) map[string]string {
	out := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}
	return out
}

// upnpDevice is the subset of a UPnP device description worth keeping. The full
// document also lists every service and control URL, which belongs to a
// control-point tool, not to a survey.
type upnpDevice struct {
	FriendlyName string `xml:"device>friendlyName"`
	Manufacturer string `xml:"device>manufacturer"`
	ModelName    string `xml:"device>modelName"`
	ModelNumber  string `xml:"device>modelNumber"`
	DeviceType   string `xml:"device>deviceType"`
	SerialNumber string `xml:"device>serialNumber"`
}

// fetchDescription GETs a device description document.
//
// The response is attributed to the IP that answered the M-SEARCH rather than
// to the host in the LOCATION URL, and the body is size-limited: this is an
// unauthenticated document from an unknown device, and a survey tool that
// happily reads 200MB because a device said Content-Length is a survey tool
// that can be hung by any device on the network.
func fetchDescription(ctx context.Context, g *model.Graph, loc, responder string) {
	u, err := url.Parse(loc)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var d upnpDevice
	body := io.LimitReader(resp.Body, 256<<10)
	if err := xml.NewDecoder(body).Decode(&d); err != nil {
		return
	}

	now := time.Now()
	ids := []model.Identity{model.IPIdentity(responder)}
	add := func(key, val string) {
		if strings.TrimSpace(val) == "" {
			return
		}
		g.Observe(ids, model.Claim{Key: key, Value: strings.TrimSpace(val), Confidence: model.Asserted,
			Source: model.Source{Method: "upnp-description", Evidence: loc, At: now}})
	}
	add("hostname", d.FriendlyName)
	add("vendor", d.Manufacturer)
	if d.ModelName != "" {
		add("model", strings.TrimSpace(d.ModelName+" "+d.ModelNumber))
	}
	if class := upnpClass(d.DeviceType); class != "" {
		add("class", class)
	}
}

// upnpClass maps a UPnP device type URN to a plain-language class.
func upnpClass(urn string) string {
	switch {
	case strings.Contains(urn, "InternetGatewayDevice"), strings.Contains(urn, "WANDevice"):
		return "router / gateway"
	case strings.Contains(urn, "MediaRenderer"):
		return "media receiver"
	case strings.Contains(urn, "MediaServer"):
		return "media server"
	case strings.Contains(urn, "Printer"):
		return "printer"
	case strings.Contains(urn, "Basic:1"):
		return ""
	}
	return ""
}

func firstToken(s string) string {
	if i := strings.IndexAny(s, " /"); i > 0 {
		return s[:i]
	}
	return s
}

func lastToken(s string) string {
	f := strings.Fields(s)
	if len(f) == 0 {
		return s
	}
	return f[len(f)-1]
}
