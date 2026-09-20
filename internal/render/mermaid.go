package render

import (
	"fmt"
	"strings"

	"github.com/Tanmay-Anand/carta/internal/drift"
	"github.com/Tanmay-Anand/carta/internal/model"
)

// Mermaid renders the topology as text.
//
// Text, not an image, for three reasons: it renders inline on GitHub and in
// every wiki, it diffs line by line when the network changes, and it needs no
// GraphViz on a machine that was never prepared for this tool.
//
// The shape of a node encodes how well it is known, which is the one piece of
// information a conventional network diagram throws away. A device we can name
// is a labelled box; a device that answered and would not say what it is draws
// as a dashed circle. Nobody has to read a legend to see which parts of the map
// are solid.
func Mermaid(g *model.Graph) string {
	var b strings.Builder
	b.WriteString("%% carta topology — " + g.Vantage.Prefix + " surveyed " + g.StartedAt.Format("2006-01-02 15:04") + "\n")
	b.WriteString("%% Shape carries confidence: [box] named and identified, (round) partly known,\n")
	b.WriteString("%% ((dashed)) responded but unidentified. Nothing here is a guess dressed as a fact.\n")
	b.WriteString("flowchart TD\n")

	vantageID := ""
	gwID := ""
	for _, e := range g.Entities {
		if c, _ := e.Best("class"); c == "survey vantage" {
			vantageID = nodeID(e)
		}
	}
	if gw := g.Entity(model.IPIdentity(g.Vantage.Gateway)); gw != nil {
		gwID = nodeID(gw)
	}

	// The internet is drawn as an explicit frontier node rather than implied by
	// an arrow into nowhere: the edge of what was surveyed is part of the map.
	if gwID != "" {
		b.WriteString("    INTERNET{{\"the internet<br/><small>beyond the survey</small>\"}}\n")
		fmt.Fprintf(&b, "    %s -.->|default route| INTERNET\n", gwID)
	}

	for _, e := range g.Entities {
		if v, _ := e.Best("alive"); v != "yes" {
			continue
		}
		fmt.Fprintf(&b, "    %s\n", nodeDecl(e))
	}

	for _, ed := range g.Edges {
		switch ed.Kind {
		case model.EdgeGateway:
			// Gateway edges are the spine of the diagram; vantage edges would
			// draw a line from the laptop to everything, which is true and
			// useless.
			from, to := g.Entity(model.Identity(ed.From)), g.Entity(model.Identity(ed.To))
			if from == nil || to == nil || nodeID(to) == vantageID {
				continue
			}
			style := "-->"
			if ed.Confidence <= model.Moderate {
				style = "-.->" // a dashed line for an inferred relationship
			}
			fmt.Fprintf(&b, "    %s %s %s\n", nodeID(from), style, nodeID(to))
		}
	}
	if vantageID != "" && gwID != "" {
		fmt.Fprintf(&b, "    %s ==>|survey taken here| %s\n", vantageID, gwID)
	}

	// Unswept space is drawn as a note, not omitted. A diagram that shows 14
	// devices on a /24 without saying that 240 addresses went unanswered is
	// telling the reader the network is small.
	for _, c := range g.Coverage {
		unknown := c.AddrsTotal - c.Responded
		if unknown > 0 {
			fmt.Fprintf(&b, "    UNKNOWN[\"%d addresses in %s<br/><small>no response — unknown, not empty</small>\"]\n", unknown, c.Prefix)
			b.WriteString("    class UNKNOWN unknownspace\n")
		}
	}

	b.WriteString("\n    classDef unknownspace fill:none,stroke:#999,stroke-dasharray:4 4,color:#666\n")
	b.WriteString("    classDef unidentified fill:none,stroke:#b58900,stroke-dasharray:3 3\n")
	b.WriteString("    classDef named fill:#f6f8fa,stroke:#24292f\n")
	b.WriteString("    classDef infra fill:#eef6ff,stroke:#0969da,stroke-width:2px\n")

	for _, e := range g.Entities {
		if v, _ := e.Best("alive"); v != "yes" {
			continue
		}
		fmt.Fprintf(&b, "    class %s %s\n", nodeID(e), styleClass(e))
	}
	return b.String()
}

func nodeDecl(e *model.Entity) string {
	id := nodeID(e)
	label := mermaidLabel(e)
	host, _ := e.Best("hostname")
	class, _ := e.Best("class")

	switch {
	case host == "" && class == "":
		return fmt.Sprintf("%s((\"%s\"))", id, label) // unidentified: a circle, deliberately weightless
	case host == "":
		return fmt.Sprintf("%s(\"%s\")", id, label) // partly known: rounded
	default:
		return fmt.Sprintf("%s[\"%s\"]", id, label) // named: a solid box
	}
}

func styleClass(e *model.Entity) string {
	host, _ := e.Best("hostname")
	class, _ := e.Best("class")
	switch {
	case class == "router / gateway", class == "survey vantage":
		return "infra"
	case host == "" && class == "":
		return "unidentified"
	default:
		return "named"
	}
}

// mermaidLabel builds a two-line label: what it is called, then what it is,
// then the confidence of that identification. Unknowns say "unidentified"
// rather than showing an empty line.
func mermaidLabel(e *model.Entity) string {
	addr := "—"
	if a := e.Addresses(); len(a) > 0 {
		addr = a[0]
	}
	host, hostConf := e.Best("hostname")
	class, _ := e.Best("class")
	vendor, _ := e.Best("vendor")

	top := host
	if top == "" {
		top = addr
	}
	var bottom string
	switch {
	case class != "" && vendor != "":
		bottom = vendor + " · " + class
	case class != "":
		bottom = class
	case vendor != "":
		bottom = vendor + " (from MAC prefix)"
	default:
		bottom = "unidentified"
	}

	label := mermaidEscape(top) + "<br/><small>" + mermaidEscape(bottom)
	if host != "" {
		label += " · " + hostConf.Label()
	}
	if host != "" && addr != "—" {
		label += "<br/>" + addr
	}
	return label + "</small>"
}

func nodeID(e *model.Entity) string {
	s := e.ID
	var b strings.Builder
	b.WriteString("n")
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func mermaidEscape(s string) string {
	return strings.NewReplacer(`"`, "'", "[", "(", "]", ")", "{", "(", "}", ")", "|", "/", "<", "", ">", "").Replace(s)
}

// Drift renders a comparison between two surveys.
func Drift(r drift.Report) string {
	var b strings.Builder
	b.WriteString("# Drift\n\n")
	fmt.Fprintf(&b, "`%s` → `%s` on %s\n\n", r.BeforeID, r.AfterID, r.Vantage)

	if len(r.Caveats) > 0 {
		b.WriteString("> **Read this first.**\n>\n")
		for _, c := range r.Caveats {
			fmt.Fprintf(&b, "> - %s\n", c)
		}
		b.WriteString("\n")
	}
	if len(r.Changes) == 0 {
		b.WriteString("Nothing changed.\n")
		return b.String()
	}

	sections := []struct{ kind, title, blurb string }{
		{"appeared", "Appeared", "Present now, absent from the baseline. On a network with DHCP, a device that was merely switched off last time looks identical to a new one."},
		{"vanished", "Vanished", "In the baseline, absent now. Absent is not gone: a sleeping laptop and a decommissioned server are indistinguishable from here."},
		{"moved", "Moved", "The same device at a different address, matched on a durable identity."},
		{"mutated", "Changed", "Same device, different attributes."},
	}
	for _, s := range sections {
		var rows []drift.Change
		for _, c := range r.Changes {
			if c.Kind == s.kind {
				rows = append(rows, c)
			}
		}
		if len(rows) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s (%d)\n\n%s\n\n", s.title, len(rows), s.blurb)
		for _, c := range rows {
			switch c.Kind {
			case "moved":
				fmt.Fprintf(&b, "- **%s** — `%s` → `%s` (matched by %s)\n", escape(c.Entity), c.Was, c.Now, c.MatchedBy)
			case "mutated":
				fmt.Fprintf(&b, "- **%s** (matched by %s)\n", escape(c.Entity), c.MatchedBy)
				for _, d := range c.Details {
					fmt.Fprintf(&b, "  - %s\n", escape(d))
				}
			case "vanished":
				fmt.Fprintf(&b, "- **%s** — was %s\n", escape(c.Entity), escape(c.Was))
			default:
				fmt.Fprintf(&b, "- **%s** — %s\n", escape(c.Entity), escape(c.Now))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}
