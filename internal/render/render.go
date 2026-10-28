// Package render turns a model into the documents a person reads.
//
// One rule governs every renderer here: a rendered claim always travels with
// its confidence, and anything that was not observed is shown as not observed
// rather than left blank. A blank cell in an inventory reads as "nothing there"
// to every human being who has ever seen a spreadsheet, and that is precisely
// the lie this tool exists to avoid telling.
package render

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Tanmay-Anand/carta/internal/model"
	"github.com/Tanmay-Anand/carta/internal/orient"
)

// Pack writes the complete document set and returns the paths written.
func Pack(dir string, g *model.Graph, prof *orient.Profile) ([]string, error) {
	files := map[string]string{
		"README.md":     AsBuilt(g, prof),
		"inventory.md":  Inventory(g),
		"inventory.csv": InventoryCSV(g),
		"topology.mmd":  Mermaid(g),
		"addressing.md": Addressing(g, prof),
		"findings.md":   Findings(g),
		"provenance.md": Provenance(g),
	}
	var written []string
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return written, err
		}
		written = append(written, p)
	}
	sort.Strings(written)
	return written, nil
}

// AsBuilt is the front page: what this is, where it was taken, and what it is
// allowed to claim.
func AsBuilt(g *model.Graph, prof *orient.Profile) string {
	var b strings.Builder
	v := g.Vantage

	fmt.Fprintf(&b, "# As-built: %s\n\n", v.Prefix)
	fmt.Fprintf(&b, "Surveyed %s from `%s` (%s) on %s.\n\n",
		g.StartedAt.Format("2 January 2006 at 15:04"), v.Host, v.IP, v.Interface)

	b.WriteString("> Every statement in this pack carries the evidence it rests on and how far that\n")
	b.WriteString("> evidence goes. **observed** was seen directly, **asserted** was claimed by the\n")
	b.WriteString("> device itself, **inferred** was deduced, **weak** is a guess worth checking.\n")
	b.WriteString("> Addresses that did not answer are recorded as unknown, never as empty.\n\n")

	b.WriteString("## The vantage\n\n")
	b.WriteString("| | |\n|---|---|\n")
	row := func(k, val string) {
		if val != "" {
			fmt.Fprintf(&b, "| %s | %s |\n", k, escape(val))
		}
	}
	row("Host", v.Host)
	row("Platform", v.OS)
	row("Interface", v.Interface)
	row("Address", v.IP)
	row("Prefix", v.Prefix)
	row("Gateway", v.Gateway)
	row("Resolvers", strings.Join(v.Resolvers, ", "))
	row("Search domains", strings.Join(v.SearchList, ", "))
	row("Wireless", v.Wireless)
	row("Tier", g.Meta["tier"])
	b.WriteString("\n")

	if len(v.Confounders) > 0 {
		b.WriteString("### Known bias in this instrument\n\n")
		b.WriteString("The host is a measuring device with its own distortions. These were present during the survey:\n\n")
		for _, c := range v.Confounders {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Coverage\n\n")
	for _, c := range g.Coverage {
		fmt.Fprintf(&b, "**%s** — %d of %d addresses swept, %d responded, in %s.\n\n",
			c.Prefix, c.AddrsSwept, c.AddrsTotal, c.Responded, c.EndedAt.Sub(c.StartedAt).Round(time.Millisecond))
		if len(c.Protocols) > 0 {
			fmt.Fprintf(&b, "Protocols queried: %s.\n\n", strings.Join(c.Protocols, ", "))
		}
		if len(c.PortsProbed) > 0 {
			fmt.Fprintf(&b, "TCP ports probed on each responding address: %s.\n\n", joinInts(c.PortsProbed))
		}
		if !c.Complete() {
			fmt.Fprintf(&b, "⚠ Coverage is incomplete. The unswept part of %s is **unknown**, not empty.\n\n", c.Prefix)
		}
		for _, n := range c.Notes {
			fmt.Fprintf(&b, "- %s\n", n)
		}
		if len(c.Frontier) > 0 {
			b.WriteString("\n**Frontier** — seen, deliberately not surveyed:\n\n")
			for _, f := range c.Frontier {
				fmt.Fprintf(&b, "- %s\n", f)
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("## The pack\n\n")
	b.WriteString("| File | What it holds |\n|---|---|\n")
	b.WriteString("| `inventory.md` | Every device, what it is, and how confident that is |\n")
	b.WriteString("| `inventory.csv` | The same, for a spreadsheet |\n")
	b.WriteString("| `topology.mmd` | The network as a Mermaid diagram |\n")
	b.WriteString("| `addressing.md` | Addressing, naming, and who serves what |\n")
	b.WriteString("| `findings.md` | What is worth doing something about |\n")
	b.WriteString("| `provenance.md` | Every claim, its source, and the merges behind each device |\n")
	b.WriteString("| `survey.json` | The snapshot a future survey diffs against |\n")
	b.WriteString("| `survey.db` | The same survey, queryable in SQLite |\n")
	return b.String()
}

// Inventory is the table people actually use.
func Inventory(g *model.Graph) string {
	var b strings.Builder
	b.WriteString("# Device inventory\n\n")
	fmt.Fprintf(&b, "%s, surveyed %s\n\n", g.Vantage.Prefix, g.StartedAt.Format("2 January 2006 15:04"))
	b.WriteString("Confidence is shown for every identifying claim. `—` means the survey did not learn it,\n")
	b.WriteString("which is different from the device not having one.\n\n")
	b.WriteString("| Address | MAC | Name | Vendor | Class | Services | Evidence |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")

	for _, e := range g.Entities {
		if v, _ := e.Best("alive"); v != "yes" {
			continue
		}
		host, hostConf := e.Best("hostname")
		vendor, vendorConf := e.Best("vendor")
		class, classConf := e.Best("class")

		var services []string
		for _, c := range e.All("service") {
			services = append(services, c.Value)
		}
		if len(services) > 4 {
			services = append(services[:4], fmt.Sprintf("+%d more", len(services)-4))
		}

		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n",
			codeJoin(e.Addresses()),
			codeJoin(e.MACs()),
			withConf(host, hostConf),
			withConf(vendor, vendorConf),
			withConf(class, classConf),
			escape(strings.Join(services, ", ")),
			methodSummary(e),
		)
	}

	// Devices that were probed and said nothing are listed separately rather
	// than omitted: "we looked and found nothing" is a result.
	var silent []string
	for _, e := range g.Entities {
		if v, _ := e.Best("alive"); v == "yes" {
			if p, _ := e.Best("probed"); p != "" {
				if host, _ := e.Best("hostname"); host == "" {
					silent = append(silent, strings.Join(e.Addresses(), ", "))
				}
			}
		}
	}
	if len(silent) > 0 {
		b.WriteString("\n## Responded, but revealed nothing\n\n")
		b.WriteString("These addresses answered at the network layer and then declined to identify themselves.\n")
		b.WriteString("They are devices; what kind is unknown.\n\n")
		for _, s := range silent {
			fmt.Fprintf(&b, "- `%s`\n", s)
		}
	}

	if conflicts := allConflicts(g); len(conflicts) > 0 {
		b.WriteString("\n## Disagreements\n\n")
		b.WriteString("Two sources claimed different things about the same device. Both are kept; neither was silently discarded.\n\n")
		for _, c := range conflicts {
			fmt.Fprintf(&b, "- %s\n", escape(c))
		}
	}
	return b.String()
}

// InventoryCSV is the same data for a spreadsheet. Confidence travels in its
// own column per field, because a CSV that drops it is a CSV that will be
// pasted into a slide deck as fact.
func InventoryCSV(g *model.Graph) string {
	var b strings.Builder
	b.WriteString("address,mac,hostname,hostname_confidence,vendor,vendor_confidence,class,class_confidence,services,methods\n")
	for _, e := range g.Entities {
		if v, _ := e.Best("alive"); v != "yes" {
			continue
		}
		host, hostConf := e.Best("hostname")
		vendor, vendorConf := e.Best("vendor")
		class, classConf := e.Best("class")
		var services []string
		for _, c := range e.All("service") {
			services = append(services, c.Value)
		}
		fields := []string{
			strings.Join(e.Addresses(), " "), strings.Join(e.MACs(), " "),
			host, confOrEmpty(host, hostConf), vendor, confOrEmpty(vendor, vendorConf),
			class, confOrEmpty(class, classConf), strings.Join(services, "; "), methodsOf(e),
		}
		for i, f := range fields {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(csvQuote(f))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// Addressing is the plan nobody wrote down.
func Addressing(g *model.Graph, prof *orient.Profile) string {
	var b strings.Builder
	b.WriteString("# Addressing and naming\n\n")
	for _, c := range g.Coverage {
		fmt.Fprintf(&b, "## %s\n\n", c.Prefix)
		fmt.Fprintf(&b, "- Usable addresses: %d\n", c.AddrsTotal)
		fmt.Fprintf(&b, "- Responded: %d (%d%% occupancy of the swept range)\n", c.Responded, pct(c.Responded, max(c.AddrsSwept, 1)))
		fmt.Fprintf(&b, "- Gateway: `%s`\n", g.Vantage.Gateway)
		if prof.DHCPServer != "" {
			fmt.Fprintf(&b, "- DHCP server: `%s`\n", prof.DHCPServer)
		}
		if len(g.Vantage.Resolvers) > 0 {
			fmt.Fprintf(&b, "- Resolvers: `%s`\n", strings.Join(g.Vantage.Resolvers, "`, `"))
		}
		if cov, ok := g.Meta["ptr_coverage"]; ok {
			fmt.Fprintf(&b, "- Reverse DNS coverage: %s responding devices have a PTR record\n", cov)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Occupancy\n\n")
	b.WriteString("What is at each address that answered. Gaps are addresses that did not answer — unknown, not free.\n\n")
	b.WriteString("| Address | What answered |\n|---|---|\n")
	for _, e := range g.Entities {
		if v, _ := e.Best("alive"); v != "yes" {
			continue
		}
		for _, a := range e.Addresses() {
			fmt.Fprintf(&b, "| `%s` | %s |\n", a, escape(e.Label()))
		}
	}

	b.WriteString("\n## Local interfaces on the vantage host\n\n")
	b.WriteString("| Interface | MAC | Addresses | State |\n|---|---|---|---|\n")
	for _, i := range prof.Interfaces {
		state := "down"
		if i.Up {
			state = "up"
		}
		if len(i.Addrs) == 0 && !i.Up {
			continue
		}
		fmt.Fprintf(&b, "| %s | `%s` | %s | %s |\n", escape(i.Name), i.MAC, codeJoin(i.Addrs), state)
	}
	return b.String()
}

// Findings is the actionable half of the pack.
func Findings(g *model.Graph) string {
	var b strings.Builder
	b.WriteString("# Findings\n\n")
	if len(g.Findings) == 0 {
		b.WriteString("Nothing worth reporting.\n")
		return b.String()
	}
	for _, f := range g.Findings {
		fmt.Fprintf(&b, "## %s %s\n\n", severityMark(f.Severity), f.Title)
		fmt.Fprintf(&b, "*%s*\n\n", f.Confidence.Label())
		b.WriteString(f.Detail + "\n\n")
	}
	return b.String()
}

// Provenance is the audit trail: every claim, where it came from, and every
// merge that decided two observations were one device.
func Provenance(g *model.Graph) string {
	var b strings.Builder
	b.WriteString("# Provenance\n\n")
	b.WriteString("Everything the survey believes, and why. This file exists so a wrong entry in the\n")
	b.WriteString("inventory can be traced to the observation that produced it.\n\n")

	for _, e := range g.Entities {
		fmt.Fprintf(&b, "## %s\n\n", escape(e.Label()))
		fmt.Fprintf(&b, "Identities: %s\n\n", codeJoin(identityStrings(e)))
		b.WriteString("| Claim | Value | Confidence | Method | Evidence |\n|---|---|---|---|---|\n")
		for _, c := range e.Claims {
			fmt.Fprintf(&b, "| %s | %s | %s | `%s` | %s |\n",
				c.Key, escape(c.Value), c.Confidence.Label(), c.Source.Method, escape(c.Source.Evidence))
		}
		if len(e.Merges) > 0 {
			b.WriteString("\n**Merged because:**\n\n")
			for _, m := range e.Merges {
				fmt.Fprintf(&b, "- %s\n", escape(m))
			}
		}
		if len(e.Conflicts) > 0 {
			b.WriteString("\n**Disagreements:**\n\n")
			for _, c := range e.Conflicts {
				fmt.Fprintf(&b, "- %s\n", escape(c))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

// helpers

func withConf(v string, c model.Confidence) string {
	if v == "" {
		return "—"
	}
	return fmt.Sprintf("%s <sub>%s</sub>", escape(v), c.Label())
}

func confOrEmpty(v string, c model.Confidence) string {
	if v == "" {
		return ""
	}
	return c.Label()
}

func methodSummary(e *model.Entity) string {
	m := methodsOf(e)
	if m == "" {
		return "—"
	}
	return "`" + strings.ReplaceAll(m, " ", "` `") + "`"
}

func methodsOf(e *model.Entity) string {
	seen := map[string]bool{}
	var out []string
	for _, c := range e.Claims {
		if !seen[c.Source.Method] {
			seen[c.Source.Method] = true
			out = append(out, c.Source.Method)
		}
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

func identityStrings(e *model.Entity) []string {
	out := make([]string, 0, len(e.Identities))
	for _, id := range e.Identities {
		out = append(out, string(id))
	}
	return out
}

func allConflicts(g *model.Graph) []string {
	var out []string
	for _, e := range g.Entities {
		for _, c := range e.Conflicts {
			out = append(out, e.Label()+" — "+c)
		}
	}
	return out
}

func severityMark(s string) string {
	switch s {
	case "risk":
		return "🔴"
	case "warn":
		return "🟠"
	default:
		return "•"
	}
}

func codeJoin(xs []string) string {
	if len(xs) == 0 {
		return "—"
	}
	return "`" + strings.Join(xs, "` `") + "`"
}

func joinInts(xs []int) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = fmt.Sprint(x)
	}
	return strings.Join(parts, ", ")
}

func escape(s string) string {
	return strings.NewReplacer("|", `\|`, "\n", " ", "\r", "").Replace(s)
}

func csvQuote(s string) string {
	if strings.ContainsAny(s, `",`+"\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func pct(a, b int) int {
	if b == 0 {
		return 0
	}
	return a * 100 / b
}
