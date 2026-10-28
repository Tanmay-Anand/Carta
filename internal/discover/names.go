package discover

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Tanmay-Anand/carta/internal/model"
)

// ReverseDNS asks the network's own resolver what it calls each address.
//
// A PTR record is a Moderate claim, never better: it records what somebody once
// typed into a zone file or what DHCP registered months ago, and stale PTRs
// outlive the devices they named. It is still worth having, because on a
// managed network it is the only source that reflects the operator's intent
// rather than the device's self-description.
//
// PTR coverage is also a documentation-quality metric in its own right. A /24
// with four PTR records is a network nobody is administering by name.
func ReverseDNS(ctx context.Context, g *model.Graph, targets []string, resolvers []string, timeout time.Duration) (found, asked int) {
	// Ask the network's resolvers directly, in the order the vantage listed
	// them, rather than the host's stub, which may be a VPN's, a corporate
	// split-DNS, or (Android with Private DNS) a public resolver over TLS — none
	// of which know anything about this segment's PTR records.
	pool := []*net.Resolver{net.DefaultResolver}
	if len(resolvers) > 0 {
		pool = pool[:0]
		for _, r := range resolvers {
			addr := net.JoinHostPort(r, "53")
			pool = append(pool, &net.Resolver{
				PreferGo: true,
				Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
					d := net.Dialer{Timeout: timeout}
					return d.DialContext(ctx, network, addr)
				},
			})
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 32)

	for _, ip := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			names, err := lookupAddr(ctx, pool, ip, timeout*3)
			mu.Lock()
			asked++
			mu.Unlock()
			if err != nil || len(names) == 0 {
				return
			}
			mu.Lock()
			found++
			mu.Unlock()
			name := strings.TrimSuffix(names[0], ".")
			g.Observe([]model.Identity{model.IPIdentity(ip), model.HostIdentity(name)},
				model.Claim{Key: "hostname", Value: name, Confidence: model.Moderate,
					Source: model.Source{Method: "rdns", Evidence: "PTR record for " + ip, At: time.Now()}})
		}(ip)
	}
	wg.Wait()
	return found, asked
}

// lookupAddr asks each resolver in turn. A resolver that answers "no such
// name" has answered, so the next is not asked; one that times out or refuses
// is skipped, because the first resolver a vantage lists is not always the one
// that serves this segment.
func lookupAddr(ctx context.Context, pool []*net.Resolver, ip string, budget time.Duration) ([]string, error) {
	var err error
	for _, r := range pool {
		c, cancel := context.WithTimeout(ctx, budget)
		var names []string
		names, err = r.LookupAddr(c, ip)
		cancel()
		if err == nil {
			return names, nil
		}
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, err
		}
	}
	return nil, err
}

// OUI lookup.
//
// The vendor half of a MAC address is the most available identity signal on a
// network and one of the least reliable, which is exactly why it must be
// carried at Moderate and never higher. Prefixes get resold, virtual machines
// inherit their hypervisor's prefix, and every modern phone and laptop
// randomises its address on networks it does not trust — in which case the
// honest answer is not a vendor at all but "this address is randomised", which
// is itself worth reporting.

// Vendors annotates every entity that has a MAC with what its OUI suggests.
func Vendors(g *model.Graph, table map[string]string) {
	now := time.Now()
	// The MAC list is collected before any claim is added, because adding one
	// can merge two entities and reshape the slice being walked.
	var macs []string
	for _, e := range g.Entities {
		macs = append(macs, e.MACs()...)
	}
	{
		for _, mac := range macs {
			if locallyAdministered(mac) {
				g.Observe([]model.Identity{model.MACIdentity(mac)},
					model.Claim{Key: "note", Value: "MAC address is randomised or locally assigned — no vendor can be inferred, and it will differ on the next visit", Confidence: model.High,
						Source: model.Source{Method: "oui", Evidence: "the second-least-significant bit of the first octet is set", At: now}},
					// Randomisation is itself weak evidence of what the device
					// is: phones, tablets and laptops do it by default, while
					// printers, cameras and routers do not. Weak, and labelled
					// weak, but better than an empty class column on half a
					// home network.
					model.Claim{Key: "class", Value: "personal device (randomised MAC)", Confidence: model.Low,
						Source: model.Source{Method: "oui", Evidence: "address randomisation is a phone and laptop behaviour", At: now}})
				continue
			}
			prefix := strings.ToUpper(strings.ReplaceAll(mac, ":", ""))
			if len(prefix) < 6 {
				continue
			}
			if vendor, ok := table[prefix[:6]]; ok {
				g.Observe([]model.Identity{model.MACIdentity(mac)},
					model.Claim{Key: "vendor", Value: vendor, Confidence: model.Moderate,
						Source: model.Source{Method: "oui", Evidence: "IEEE OUI " + prefix[:6] + " — prefixes are resold and inherited by virtual machines", At: now}})
			}
		}
	}
}

// locallyAdministered reports whether a MAC was assigned by software rather
// than burned in by a manufacturer. Every randomised phone address has this bit
// set, and it is the difference between "unknown vendor" and "deliberately
// anonymous device".
func locallyAdministered(mac string) bool {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) == 0 {
		return false
	}
	return hw[0]&0x02 != 0
}

// LoadOUI reads an IEEE OUI table from disk if one is present, falling back to
// the small built-in table.
//
// The full IEEE registry is ~35,000 entries and changes weekly. Embedding it
// would add megabytes to a binary whose whole pitch is that it fits on a stick
// and runs anywhere, and it would be stale the month after release. So the
// binary carries the prefixes that cover a typical small network, and a
// site-specific or current registry can be dropped alongside it.
//
// Format: the IEEE's own oui.csv, or lines of "AABBCC<tab>Vendor Name".
func LoadOUI(path string) (map[string]string, error) {
	table := make(map[string]string, len(builtinOUI))
	for k, v := range builtinOUI {
		table[k] = v
	}
	if path == "" {
		return table, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return table, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var prefix, vendor string
		if strings.Contains(line, ",") { // IEEE oui.csv: Registry,Assignment,Organization Name,Address
			// Parsed as real CSV rather than split on commas: a third of the
			// registry's organisation names contain a comma ("Example
			// Networks, Inc"), and splitting truncates every one of them.
			rec, err := csv.NewReader(strings.NewReader(line)).Read()
			if err != nil || len(rec) < 3 || len(rec[1]) < 6 {
				continue
			}
			prefix, vendor = rec[1], rec[2]
		} else {
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			prefix, vendor = parts[0], strings.Join(parts[1:], " ")
		}
		prefix = strings.ToUpper(strings.NewReplacer(":", "", "-", "").Replace(prefix))
		if len(prefix) >= 6 && vendor != "" {
			table[prefix[:6]] = vendor
		}
	}
	return table, sc.Err()
}

// builtinOUI covers the prefixes that actually turn up on a home, small-office
// or branch network: the phone and laptop vendors, the common router and IoT
// makers, and the hypervisor ranges that explain a "device" that is really a
// virtual machine.
var builtinOUI = map[string]string{
	// Hypervisors and container fabrics — these prevent a VM being reported as
	// a mystery device, which is the most common false discovery on a laptop.
	"005056": "VMware", "000C29": "VMware", "001C14": "VMware", "000569": "VMware",
	"00155D": "Microsoft (Hyper-V)", "000D3A": "Microsoft (Azure)", "0003FF": "Microsoft (virtual)",
	"080027": "Oracle VirtualBox", "0A0027": "Oracle VirtualBox",
	"525400": "QEMU/KVM", "FA163E": "OpenStack",
	"02420A": "Docker", "0242AC": "Docker",
	// Apple
	"DCA904": "Apple", "F0989D": "Apple", "A4B197": "Apple", "8C8590": "Apple", "3C0754": "Apple",
	"D0817A": "Apple", "F82793": "Apple", "9C207B": "Apple", "A85C2C": "Apple", "04D3CF": "Apple",
	// Samsung
	"D0176A": "Samsung", "5CF6DC": "Samsung", "8C7712": "Samsung", "F008F1": "Samsung", "C81479": "Samsung",
	// Common phone and laptop silicon
	"DC567B": "MediaTek", "F4F5E8": "Google", "3C5AB4": "Google", "A47733": "Google",
	"7C7635": "Xiaomi", "64B473": "Xiaomi", "F8A45F": "Xiaomi", "286C07": "Xiaomi",
	"E0DCFF": "OnePlus", "C0EEFB": "OnePlus", "94652D": "OnePlus",
	"001A11": "Google", "F4F26D": "TP-Link", "5C628B": "TP-Link", "B0BE76": "TP-Link",
	"1C61B4": "Realme/Oppo", "D8C4E9": "Oppo", "48A9D2": "Vivo",
	"F48E38": "Dell", "B083FE": "Dell", "185A58": "Dell", "8CEC4B": "Dell",
	"3417EB": "Dell", "0026B9": "Dell", "A4BB6D": "Intel", "8C554A": "Intel", "E4A471": "Intel",
	"B4B686": "HP", "3CD92B": "HP", "9C8E99": "HP", "0017A4": "HP", "001B78": "HP",
	"D89EF3": "Dell", "F0D4E2": "Dell", "C025A5": "Lenovo", "54EE75": "Lenovo", "8CB84A": "ASUS",
	"9C5C8E": "ASUS", "2C4D54": "ASUS", "F832E4": "ASUS", "1C872C": "ASUS",
	// Routers, switches and access points
	"B8FBB3": "TP-Link", "C83A35": "Tenda", "50C7BF": "TP-Link", "14CC20": "TP-Link",
	"E8DE27": "TP-Link", "9C53CD": "TP-Link", "002722": "Ubiquiti", "24A43C": "Ubiquiti",
	"788A20": "Ubiquiti", "F09FC2": "Ubiquiti", "4C5E0C": "MikroTik", "6C3B6B": "MikroTik",
	"E48D8C": "MikroTik", "000C42": "MikroTik", "2CC81B": "Cisco", "001A2F": "Cisco",
	"F4CFE2": "Cisco", "001B54": "Cisco", "001DA2": "Netgear", "A00460": "Netgear",
	"204E7F": "Netgear", "9C3DCF": "Netgear", "1C3BF3": "D-Link", "28107B": "D-Link",
	"001CF0": "D-Link", "C8BE19": "Zyxel", "5CF4AB": "Zyxel", "E0286D": "AVM (FRITZ!Box)",
	// NAS, printers, cameras, media
	"0011D8": "Synology", "001132": "Synology", "245EBE": "QNAP",
	"3C2AF4": "Brother", "008077": "Brother", "002673": "Brother",
	"0000AA": "Xerox", "9CB654": "Canon", "88AE1D": "Canon",
	"000174": "Canon", "AC1826": "Epson", "001B21": "Intel",
	"BCDDC2": "Espressif (ESP32)", "240AC4": "Espressif (ESP32)", "8CAAB5": "Espressif (ESP32)",
	"A020A6": "Espressif (ESP8266)", "5CCF7F": "Espressif (ESP8266)", "DC4F22": "Espressif",
	"B827EB": "Raspberry Pi", "DCA632": "Raspberry Pi", "E45F01": "Raspberry Pi", "D83ADD": "Raspberry Pi",
	"2CCF67": "Raspberry Pi", "00248C": "ASUSTek", "001E8F": "Canon",
	"4C11AE": "Espressif", "54AF97": "TP-Link (Tapo)", "3C84B5": "Sonoff/ITEAD",
	"B0A737": "Roku", "CC6DA0": "Roku", "D83134": "Roku",
	"54604D": "Sonos", "B8E937": "Sonos", "5CAAFD": "Sonos", "347E5C": "Sonos",
	"18B430": "Nest", "64166D": "Cisco", "F4F5D8": "Google (Chromecast)", "6C5AB0": "TCL",
	"CC9EA2": "Amazon", "FC65DE": "Amazon", "44650D": "Amazon", "74C246": "Amazon",
	"0CDC7E": "Espressif", "ACBC32": "Apple", "C0D012": "Hikvision", "4C11BF": "Hikvision",
	"BCAD28": "Hikvision", "E0508B": "Dahua", "3C1B94": "Dahua", "9C144D": "Dahua",
	"00408C": "Axis", "ACCC8E": "Axis", "B4A382": "Axis",
}
