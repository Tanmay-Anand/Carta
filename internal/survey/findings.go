package survey

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Tanmay-Anand/carta/internal/model"
	"github.com/Tanmay-Anand/carta/internal/orient"
)

// Findings are the part of a survey a person acts on, so the bar for including
// one is that a reader could do something about it. "Port 80 is open" is not a
// finding. "A device is offering a management interface that asked for no
// credentials" is.
//
// Every finding carries its own confidence for the same reason claims do: a
// warning derived from a weak inference, printed with the same authority as one
// derived from an observation, teaches the reader to ignore all of them.

// plural renders "1 device" and "2 devices". A report that says "1 devices"
// reads as machine output, and machine output is what people skim past.
func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func findings(g *model.Graph, prof *orient.Profile, cov model.Coverage) {
	unidentified := 0
	randomised := 0
	var plaintext, expired, openMgmt, nbnsHosts []string

	for _, e := range g.Entities {
		label := e.Label()

		// Unidentified: alive, but nothing beyond an address and possibly a
		// vendor guess. On a network nobody documents, this number is the
		// single most useful line in the report.
		host, _ := e.Best("hostname")
		class, classConf := e.Best("class")
		// A weak guess is not an identification. A device classed only as
		// "personal device (randomised MAC)" or "something on port 80" is still
		// a device nobody can name, and the count has to say so.
		if host == "" && (class == "" || classConf <= model.Low) {
			if v, _ := e.Best("alive"); v == "yes" {
				unidentified++
			}
		}
		for _, c := range e.All("note") {
			switch {
			case strings.HasPrefix(c.Value, "MAC address is randomised"):
				randomised++
			case strings.HasPrefix(c.Value, "TLS certificate expired"):
				expired = append(expired, label+" — "+c.Value)
			}
		}
		for _, c := range e.All("service") {
			switch {
			case strings.HasPrefix(c.Value, "telnet"), strings.HasPrefix(c.Value, "ftp"):
				plaintext = append(plaintext, label+" — "+c.Value)
			}
		}
		// A management interface that returned a page and never asked for
		// credentials. The absence of WWW-Authenticate is not proof that it is
		// unauthenticated — most modern devices use a login form and return 200
		// — so this is Moderate and says so.
		title, _ := e.Best("http-title")
		if title != "" && !hasNotePrefix(e, "HTTP authentication required") {
			openMgmt = append(openMgmt, label+" — \""+title+"\"")
		}
		for _, c := range e.Claims {
			if c.Source.Method == "nbns" && c.Key == "hostname" {
				nbnsHosts = append(nbnsHosts, label)
				break
			}
		}
	}

	live := 0
	for _, e := range g.Entities {
		if v, _ := e.Best("alive"); v == "yes" {
			live++
		}
	}

	g.AddFinding(model.Finding{
		Severity: "info", Confidence: model.High,
		Title:  fmt.Sprintf("%s responded on %s", plural(live, "device", "devices"), cov.Prefix),
		Detail: coverageSentence(cov),
	})

	if unidentified > 0 {
		g.AddFinding(model.Finding{
			Severity: "warn", Confidence: model.High,
			Title: fmt.Sprintf("%d of %s could not be identified", unidentified, plural(live, "device", "devices")),
			Detail: "They answered, but announced no name and no service that reveals what they are. " +
				"They are drawn as unidentified in the topology rather than given a guessed label. " +
				"Running the survey again from a wired jack, or with an up-to-date OUI pack, usually resolves some of them.",
		})
	}
	if randomised > 0 {
		g.AddFinding(model.Finding{
			Severity: "info", Confidence: model.High,
			Title: fmt.Sprintf("%s using randomised MAC addresses", plural(randomised, "device is", "devices are")),
			Detail: "Modern phones and laptops randomise their hardware address per network. Their vendor cannot be inferred, " +
				"and they will appear as different devices on a future survey — so drift reports will show them as vanished and appeared.",
		})
	}
	if len(plaintext) > 0 {
		g.AddFinding(model.Finding{
			Severity: "risk", Confidence: model.High,
			Title:    fmt.Sprintf("%s plaintext remote access", plural(len(plaintext), "device offers", "devices offer")),
			Detail:   "Telnet and FTP carry credentials in clear text on the local segment:\n  - " + strings.Join(plaintext, "\n  - "),
			Entities: plaintext,
		})
	}
	if len(openMgmt) > 0 {
		g.AddFinding(model.Finding{
			Severity: "warn", Confidence: model.Moderate,
			Title: fmt.Sprintf("%s without requesting credentials", plural(len(openMgmt), "web management interface answered", "web management interfaces answered")),
			Detail: "Each returned a page to an unauthenticated request. Many devices use a login form and still return 200, " +
				"so this is a list to check rather than a list of confirmed exposures:\n  - " + strings.Join(openMgmt, "\n  - "),
			Entities: openMgmt,
		})
	}
	if len(expired) > 0 {
		g.AddFinding(model.Finding{
			Severity: "warn", Confidence: model.High,
			Title:    fmt.Sprintf("%s an expired TLS certificate", plural(len(expired), "device serves", "devices serve")),
			Detail:   strings.Join(expired, "\n  - "),
			Entities: expired,
		})
	}
	if len(nbnsHosts) > 0 {
		g.AddFinding(model.Finding{
			Severity: "warn", Confidence: model.High,
			Title: fmt.Sprintf("%s NetBIOS name queries", plural(len(nbnsHosts), "host answers", "hosts answer")),
			Detail: "NBNS and LLMNR resolve names by broadcast, so any device on this segment can answer for a name that does not exist. " +
				"This is the mechanism behind credential-relay attacks on flat networks. Disabling NetBIOS over TCP/IP on these hosts removes it.",
			Entities: nbnsHosts,
		})
	}

	// PTR coverage as a documentation-quality metric rather than a security
	// one: it measures whether anyone is administering this network by name.
	if cov, ok := g.Meta["ptr_coverage"]; ok {
		if found, asked, ok := splitRatio(cov); ok && asked > 0 {
			pct := found * 100 / asked
			sev := "info"
			if pct < 25 {
				sev = "warn"
			}
			g.AddFinding(model.Finding{
				Severity: sev, Confidence: model.High,
				Title: fmt.Sprintf("Reverse DNS covers %d%% of responding devices (%d of %d)", pct, found, asked),
				Detail: "PTR records are how a network becomes legible to the people who operate it. " +
					"Low coverage means every future investigation starts by working out what an address is.",
			})
		}
	}

	if len(prof.Confounders) > 0 {
		g.AddFinding(model.Finding{
			Severity: "info", Confidence: model.High,
			Title:  "This survey was taken through a biased instrument",
			Detail: "  - " + strings.Join(prof.Confounders, "\n  - "),
		})
	}
	if !cov.Complete() && cov.AddrsTotal > 0 {
		g.AddFinding(model.Finding{
			Severity: "warn", Confidence: model.High,
			Title: "Coverage is incomplete",
			Detail: fmt.Sprintf("%d of %d addresses in %s were swept. The unswept part of the range is unknown, not empty.",
				cov.AddrsSwept, cov.AddrsTotal, cov.Prefix),
		})
	}
}

func coverageSentence(cov model.Coverage) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d of %d addresses in %s were swept in %s. ",
		cov.AddrsSwept, cov.AddrsTotal, cov.Prefix, cov.EndedAt.Sub(cov.StartedAt).Round(100_000_000))
	fmt.Fprintf(&b, "%d TCP ports were probed on each responding address, and %s were queried. ",
		len(cov.PortsProbed), strings.Join(cov.Protocols, ", "))
	b.WriteString("Addresses that did not respond are recorded as unknown: nothing answered, which is not the same as nothing being there.")
	return b.String()
}

func hasNotePrefix(e *model.Entity, prefix string) bool {
	for _, c := range e.All("note") {
		if strings.HasPrefix(c.Value, prefix) {
			return true
		}
	}
	return false
}

func splitRatio(s string) (a, b int, ok bool) {
	x, y, found := strings.Cut(s, "/")
	if !found {
		return 0, 0, false
	}
	a, err1 := strconv.Atoi(x)
	b, err2 := strconv.Atoi(y)
	return a, b, err1 == nil && err2 == nil
}
