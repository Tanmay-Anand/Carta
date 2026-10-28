// Package orient performs Tier P of a survey: work out where the instrument is
// standing, before it emits a single packet.
//
// Every later result is relative to this. A device list without a vantage is
// uninterpretable — "we found 14 hosts" means nothing until you know which jack
// was used, whether a VPN was carrying the default route at the time, and
// whether a host firewall was dropping the replies. So orientation runs first,
// it runs passively, and its findings annotate everything downstream.
package orient

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Tanmay-Anand/carta/internal/model"
)

// Profile is what the host knows about its own position without asking the
// network anything.
type Profile struct {
	Host        string
	OS          string
	Interface   string
	MAC         string
	IP          net.IP
	Prefix      *net.IPNet
	Gateway     string
	Resolvers   []string
	Search      []string
	DHCPServer  string
	Wireless    string
	Neighbours  []Neighbour // the ARP/NDP cache as it stands before we touch anything
	Confounders []string
	Interfaces  []InterfaceInfo

	// Instrument names what took the survey; see model.Vantage.Instrument.
	Instrument string
}

// Neighbour is one entry of the host's own ARP/NDP cache.
type Neighbour struct {
	IP    string
	MAC   string
	State string // "dynamic", "static", "stale", "reachable" — as the OS reported it
}

// InterfaceInfo is a summary of one local interface, kept for the as-built doc.
type InterfaceInfo struct {
	Name  string
	MAC   string
	Addrs []string
	Up    bool
}

// Run collects the profile. It never fails outright: a missing gateway or an
// unparseable resolver list degrades the survey, it does not end it, and what
// could not be determined is left empty rather than guessed.
func Run(ctx context.Context) (*Profile, error) {
	p := &Profile{OS: runtime.GOOS + "/" + runtime.GOARCH, Instrument: "carta-cli"}
	p.Host, _ = os.Hostname()

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("orient: enumerate interfaces: %w", err)
	}

	// Pick the interface that carries the default route. We find it by opening
	// a UDP socket towards a public address — this sends nothing, it only asks
	// the kernel which source address it would use — and then matching that
	// address back to an interface. It is the one method that is identical on
	// all three platforms and needs no elevation.
	localIP := outboundIP()

	for _, ifc := range ifaces {
		addrs, _ := ifc.Addrs()
		info := InterfaceInfo{Name: ifc.Name, MAC: ifc.HardwareAddr.String(), Up: ifc.Flags&net.FlagUp != 0}
		for _, a := range addrs {
			info.Addrs = append(info.Addrs, a.String())
		}
		p.Interfaces = append(p.Interfaces, info)

		if ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagUp == 0 {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			if localIP != nil && ipnet.IP.Equal(localIP) {
				p.Interface, p.MAC, p.IP, p.Prefix = ifc.Name, ifc.HardwareAddr.String(), ipnet.IP, ipnet
			}
		}
	}
	// Fall back to the first private IPv4 interface if the socket trick was
	// blocked (some EDR products refuse it).
	if p.IP == nil {
		for _, ifc := range ifaces {
			if ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagUp == 0 {
				continue
			}
			addrs, _ := ifc.Addrs()
			for _, a := range addrs {
				if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil && ipnet.IP.IsPrivate() {
					p.Interface, p.MAC, p.IP, p.Prefix = ifc.Name, ifc.HardwareAddr.String(), ipnet.IP, ipnet
					break
				}
			}
			if p.IP != nil {
				break
			}
		}
	}

	p.readRouting(ctx)
	p.readResolvers(ctx)
	p.Neighbours = ReadNeighbours(ctx)
	p.readWireless(ctx)
	p.detectConfounders(ctx)

	sort.Slice(p.Neighbours, func(i, j int) bool { return p.Neighbours[i].IP < p.Neighbours[j].IP })
	return p, nil
}

// outboundIP asks the kernel which source address it would use to reach the
// internet. UDP "connect" is a routing-table lookup, not a transmission: no
// packet leaves the host, which is what makes this legal under a passive-only
// mandate.
func outboundIP() net.IP {
	c, err := net.Dial("udp4", "198.51.100.1:9") // TEST-NET-2, discard port
	if err != nil {
		return nil
	}
	defer c.Close()
	if ua, ok := c.LocalAddr().(*net.UDPAddr); ok {
		return ua.IP
	}
	return nil
}

// Vantage converts the profile into the model's record of where we stood.
func (p *Profile) Vantage() model.Vantage {
	v := model.Vantage{
		Host: p.Host, Interface: p.Interface, MAC: p.MAC,
		Gateway: p.Gateway, Resolvers: p.Resolvers, SearchList: p.Search,
		Wireless: p.Wireless, Confounders: p.Confounders, OS: p.OS,
		DHCPServer: p.DHCPServer, Instrument: p.Instrument,
	}
	if p.IP != nil {
		v.IP = p.IP.String()
	}
	if p.Prefix != nil {
		// net.IPNet.String() renders the address it was built from, so an
		// interface holding 192.168.0.7/24 prints "192.168.0.7/24" — which
		// names a host, not a network, and makes two surveys of the same
		// segment from different addresses look like different networks.
		v.Prefix = (&net.IPNet{IP: p.IP.Mask(p.Prefix.Mask), Mask: p.Prefix.Mask}).String()
	}
	return v
}

// run executes a command with a timeout and returns its output, or "" if it is
// not available on this host. A missing tool is an ordinary condition — the
// point of CARTA is to work on a machine nobody prepared — so this never
// returns an error.
func run(ctx context.Context, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func fields(line string) []string { return strings.Fields(strings.TrimSpace(line)) }
