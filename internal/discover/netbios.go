package discover

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/Tanmay-Anand/carta/internal/model"
)

// NetBIOS name service is thirty years old, disabled by default on nothing, and
// still the fastest way to learn a Windows machine's name and workgroup without
// credentials. A node-status query is 50 bytes and the reply contains the name
// table the host would announce to a browser service.
//
// It is also, for the same reasons, a finding: a network answering NBNS from
// every workstation is a network where an attacker can poison name resolution.
// CARTA records both the identity and the exposure, from one packet.

// nodeStatusQuery is a fixed NBSTAT request for the wildcard name "*".
//
// The name encoding is the odd part of NetBIOS: each byte of a 16-byte,
// space-padded name is split into two nibbles and each nibble is added to 'A',
// which turns "*" into "CKAAAA...". It is constant for the wildcard, so it is
// written out here rather than computed.
var nodeStatusQuery = []byte{
	0x00, 0x00, // transaction id, overwritten per request
	0x00, 0x00, // flags: standard query
	0x00, 0x01, // one question
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x20, // name length: 32 encoded bytes
	'C', 'K', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A',
	'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 'A', 0x00,
	0x00, 0x21, // type NBSTAT
	0x00, 0x01, // class IN
}

// NetBIOS sends a node-status query to each candidate address and folds the
// replies into the graph.
func NetBIOS(ctx context.Context, g *model.Graph, targets []string, timeout time.Duration) []string {
	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		responded bool
	)
	sem := make(chan struct{}, 64)

	for _, ip := range targets {
		wg.Add(1)
		sem <- struct{}{}
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()
			names, mac := nbnsQuery(ctx, ip, timeout)
			if len(names) == 0 && mac == "" {
				return
			}
			mu.Lock()
			responded = true
			mu.Unlock()

			now := time.Now()
			ids := []model.Identity{model.IPIdentity(ip)}
			if mac != "" {
				ids = append(ids, model.MACIdentity(mac))
			}
			for _, n := range names {
				switch n.kind {
				case 0x00, 0x20: // workstation service, file server service
					if !n.group {
						g.Observe(ids, model.Claim{Key: "hostname", Value: n.name, Confidence: model.Asserted,
							Source: model.Source{Method: "nbns", Evidence: "node status name " + n.name + " <" + hex2(n.kind) + ">", At: now}})
						g.Observe(ids, model.Claim{Key: "os", Value: "Windows or SMB host", Confidence: model.Low,
							Source: model.Source{Method: "nbns", Evidence: "answers NetBIOS name service", At: now}})
					} else {
						g.Observe(ids, model.Claim{Key: "workgroup", Value: n.name, Confidence: model.Asserted,
							Source: model.Source{Method: "nbns", Evidence: "group name " + n.name, At: now}})
					}
				}
			}
			if mac != "" {
				g.Observe(ids, model.Claim{Key: "l2-present", Value: "yes", Confidence: model.High,
					Source: model.Source{Method: "nbns", Evidence: "adapter address reported in node status reply", At: now}})
			}
		}(ip)
	}
	wg.Wait()

	if responded {
		return []string{"nbns"}
	}
	return nil
}

type nbName struct {
	name  string
	kind  byte
	group bool
}

func nbnsQuery(ctx context.Context, ip string, timeout time.Duration) ([]nbName, string) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "udp4", net.JoinHostPort(ip, "137"))
	if err != nil {
		return nil, ""
	}
	defer conn.Close()

	q := make([]byte, len(nodeStatusQuery))
	copy(q, nodeStatusQuery)
	q[0], q[1] = byte(time.Now().UnixNano()), byte(time.Now().UnixNano()>>8)

	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(q); err != nil {
		return nil, ""
	}
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil || n < 57 {
		return nil, ""
	}

	// Skip the header and the echoed question, then read the answer's name
	// table: one count byte followed by 18-byte entries.
	p := 12 + 34 + 10 // header, encoded question name + type/class, answer RR header
	if p >= n {
		return nil, ""
	}
	count := int(buf[p])
	p++
	var names []nbName
	for i := 0; i < count && p+18 <= n; i++ {
		raw := strings.TrimRight(string(buf[p:p+15]), " \x00")
		kind := buf[p+15]
		flags := buf[p+16]
		if isPrintable(raw) {
			names = append(names, nbName{name: raw, kind: kind, group: flags&0x80 != 0})
		}
		p += 18
	}
	var mac string
	if p+6 <= n {
		mac = net.HardwareAddr(buf[p : p+6]).String()
		if mac == "00:00:00:00:00:00" {
			mac = ""
		}
	}
	return names, mac
}

func isPrintable(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}

func hex2(b byte) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[b>>4], digits[b&0x0f]})
}
