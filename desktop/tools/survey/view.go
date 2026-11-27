package survey

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/Tanmay-Anand/carta/desktop/kit"
	"github.com/Tanmay-Anand/carta/desktop/shell"
	"github.com/Tanmay-Anand/carta/internal/drift"
	"github.com/Tanmay-Anand/carta/internal/model"
)

// Column widths and padding of design D's desktop layout.
const (
	leftWidth  = 340
	rightWidth = 380
	colPad     = 28
)

// ---------- left column: what this survey is ----------

func (t *Tool) summary() fyne.CanvasObject {
	p := shell.Current()
	st := &t.st
	g := st.g
	if g == nil {
		return t.emptySummary()
	}
	devices := alive(g)

	head := container.NewVBox(
		kit.Light(fmt.Sprintf("%d devices", len(devices)), 48, p.FG),
		kit.Light("on "+g.Vantage.Prefix, 20, p.Dim),
		kit.Space(1, 16),
		facts(g),
		kit.Space(1, 24),
		kit.Caps("Coverage", p.Dim),
		kit.Space(1, 10),
	)

	matrix := kit.NewDotMatrix(leftWidth - 2*colPad)
	var answered, silent, unreached int
	matrix.Dots, answered, silent, unreached = coverage(g)
	if r := st.run; r.running {
		// While a survey runs the matrix shows the sweep, not the last result:
		// asked so far, and not yet asked. Which addresses answered is known
		// only when the survey ends.
		total := r.sweepTotal
		if total == 0 {
			total = len(matrix.Dots)
		}
		matrix.Dots = progressDots(total, r.sweepDone)
		answered, silent, unreached = 0, r.sweepDone, total-r.sweepDone
	}

	count := func(n int, word string) fyne.CanvasObject {
		return container.NewVBox(kit.Light(fmt.Sprint(n), 22, p.FG), kit.Text(word, 12, p.Dim))
	}
	counts := container.NewHBox(count(answered, "answered"), layout.NewSpacer(),
		count(silent, "silent"), layout.NewSpacer(), count(unreached, "unreached"))

	note := "One dot per address. Silent means it did not answer: unknown, not empty."
	if quietSurvey(g) && !st.run.running {
		note = "A quiet survey asks nothing, so every address it did not hear is unreached."
	}
	noteLabel := kit.Wrapped(note, true)
	noteLabel.SizeName = shell.SizeSmall

	top := container.NewVBox(head, matrix, kit.Space(1, 14), counts, kit.Space(1, 8), kit.Flush(noteLabel))
	// The summary scrolls inside its column on a short window rather than
	// forcing the window taller than the screen; the controls stay pinned.
	return kit.Width(leftWidth, kit.Pad(26, 24, colPad, colPad,
		container.NewBorder(nil, kit.Pad(16, 0, 0, 0, t.controls()), nil, nil, container.NewVScroll(top))))
}

// emptySummary is the first run: nothing surveyed yet, and what a survey is.
func (t *Tool) emptySummary() fyne.CanvasObject {
	p := shell.Current()
	where := "carta-surveys, next to this program"
	if t.dir != "" {
		where = t.dir
	}
	what := kit.Wrapped("Survey asks every address on the network this computer is on, and saves each survey in this folder:", true)
	folder := kit.Wrapped(where, false)
	folder.TextStyle = fyne.TextStyle{Monospace: true}
	folder.SizeName = shell.SizeEvidence
	allowed := kit.Wrapped("Survey only networks you are allowed to survey.", true)
	quiet := kit.Wrapped("Survey quietly sends nothing at all. It reads only what this computer already knows, so it finds far less.", true)
	quiet.SizeName = shell.SizeSmall
	top := container.NewVBox(
		kit.Light("No survey yet", 40, p.FG),
		kit.Space(1, 18),
		kit.Flush(what),
		kit.Space(1, 6),
		kit.Flush(folder),
		kit.Space(1, 6),
		kit.Flush(allowed),
		kit.Space(1, 14),
		kit.Flush(quiet),
	)
	return kit.Width(leftWidth, kit.Pad(26, 24, colPad, colPad,
		container.NewBorder(nil, kit.Pad(16, 0, 0, 0, t.controls()), nil, nil, container.NewVScroll(top))))
}

func facts(g *model.Graph) fyne.CanvasObject {
	p := shell.Current()
	took := "unknown"
	if len(g.Coverage) > 0 && !g.Coverage[0].EndedAt.IsZero() {
		took = fmt.Sprintf("%d s", int(g.Coverage[0].EndedAt.Sub(g.Coverage[0].StartedAt).Round(time.Second).Seconds()))
	}
	row := func(k, v string) fyne.CanvasObject {
		return container.NewHBox(kit.Width(74, kit.Mono(k, 11.5, p.Dim)), kit.Mono(v, 11.5, p.FG))
	}
	return container.NewVBox(
		row("from", g.Vantage.IP+" on "+g.Vantage.Interface),
		row("started", g.StartedAt.Local().Format("2 Jan 2006, 15:04")),
		row("took", took),
		row("gateway", g.Vantage.Gateway),
	)
}

func quietSurvey(g *model.Graph) bool {
	return g != nil && len(g.Coverage) > 0 && g.Coverage[0].AddrsSwept == 0 && strings.Contains(strings.Join(g.Coverage[0].Notes, " "), "quiet survey")
}

// controls are the status, the progress line and the buttons, pinned to the
// bottom of the left column.
func (t *Tool) controls() fyne.CanvasObject {
	p := shell.Current()
	r := t.st.run
	status := container.NewHBox()
	switch {
	case r.stopping:
		status.Add(kit.Text("Stopping…", 13, p.FG))
	case r.running && r.total > 0:
		status.Add(kit.Text(fmt.Sprintf("%s %d of %d", stageName(r.stage), r.done, r.total), 13, p.FG))
	case r.running:
		status.Add(kit.Text(stageName(r.stage)+"…", 13, p.FG))
	case r.err != "":
		status.Add(kit.Text("The survey could not run: "+r.err, 13, p.Red))
	case r.stoppedIn != "":
		status.Add(kit.Text("Stopped during "+stageName(r.stoppedIn), 13, p.FG))
		status.Add(kit.Tag("Stopped early"))
	case t.st.g == nil:
		status.Add(kit.Text("Ready", 13, p.FG))
	case quietSurvey(t.st.g):
		status.Add(kit.Text("Quiet survey: nothing was sent", 13, p.FG))
	default:
		status.Add(kit.Text("Survey complete", 13, p.FG))
	}
	where := container.NewVBox()
	if r.savedTo != "" && !r.running {
		where.Add(kit.Mono("saved in "+shortPath(r.savedTo), 10.5, p.Dim))
	}

	var line fyne.CanvasObject = kit.Space(1, 2)
	if r.running {
		pl := kit.NewProgressLine()
		pl.Value = progressValue(r)
		line = pl
	} else {
		h := canvas.NewRectangle(p.Hair)
		h.SetMinSize(fyne.NewSize(1, 2))
		line = h
	}

	var buttons fyne.CanvasObject
	if r.running {
		buttons = container.NewHBox(kit.NewPill("Stop", true, t.stop))
	} else {
		buttons = container.NewHBox(
			kit.NewPill("Survey", false, func() { t.start(false) }),
			kit.Space(14, 1),
			container.NewCenter(kit.NewTextButton("Survey quietly", 14, func() { t.start(true) })),
		)
	}
	return container.NewVBox(status, where, kit.Space(1, 12), line, kit.Space(1, 18), buttons)
}

// shortPath keeps the last two parts of a path, which is what tells one
// survey folder from another: carta-surveys/20261124T193300Z.
func shortPath(p string) string {
	parts := strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == filepath.Separator })
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return strings.Join(parts, string(filepath.Separator))
}

// progressValue places each stage on one line from start to finish, so the
// line moves forward through the whole survey rather than per stage.
func progressValue(r runState) float64 {
	switch r.stage {
	case "orient":
		return 0.05
	case "listen":
		return 0.15
	case "sweep":
		if r.total > 0 {
			return 0.2 + 0.6*float64(r.done)/float64(r.total)
		}
		return 0.2
	case "probe":
		return 0.85
	case "nbns":
		return 0.92
	case "rdns", "resolve":
		return 0.97
	}
	return 0
}

// ---------- centre: the map ----------

func (t *Tool) mapArea() fyne.CanvasObject {
	p := shell.Current()
	if t.st.g == nil {
		msg := kit.Text("The map appears after the first survey.", 14, p.Dim)
		return container.NewCenter(msg)
	}
	t.mapView.Dimmed = t.st.run.running
	t.mapView.Refresh()

	key := func(dot fyne.CanvasObject, label string) fyne.CanvasObject {
		return container.NewHBox(container.NewCenter(container.New(&fixed{fyne.NewSize(9, 9)}, dot)), kit.Text(label, 12, p.Dim))
	}
	named := canvas.NewCircle(p.FG)
	partial := canvas.NewCircle(color.Transparent)
	partial.StrokeColor, partial.StrokeWidth = p.FG, 1.3
	unknown := canvas.NewCircle(color.Transparent)
	unknown.StrokeColor, unknown.StrokeWidth = p.Amber, 1.3
	legend := container.NewHBox(key(named, "identified"), kit.Space(10, 1),
		key(partial, "partly known"), kit.Space(10, 1), key(unknown, "unidentified"))

	top := container.NewHBox(kit.Caps("Map", p.Dim), layout.NewSpacer(),
		kit.Mono("click a device, drag to pan, scroll to zoom", 10.5, p.Dim))
	bottom := container.NewHBox(legend, layout.NewSpacer(), kit.NewTextButton("Fit", 12, t.mapView.Fit))
	return container.NewStack(t.mapView, kit.Pad(20, 18, 24, 24, container.NewBorder(top, bottom, nil, nil)))
}

type fixed struct{ s fyne.Size }

func (f *fixed) MinSize([]fyne.CanvasObject) fyne.Size { return f.s }
func (f *fixed) Layout(objs []fyne.CanvasObject, s fyne.Size) {
	for _, o := range objs {
		o.Resize(s)
	}
}

// ---------- right: one panel, four tabs ----------

func (t *Tool) panel() fyne.CanvasObject {
	st := &t.st
	if st.g == nil {
		msg := kit.Wrapped("Each device the survey finds is listed here, with every claim about it and the evidence for it.", true)
		return kit.Width(rightWidth, kit.Pad(30, 30, colPad, colPad, container.NewVBox(kit.Flush(msg))))
	}
	risk, findings := false, 0
	for _, f := range st.g.Findings {
		findings++
		risk = risk || f.Severity == "risk"
	}
	items := []kit.Tab{
		{Label: "Device"},
		{Label: "Findings", Count: fmt.Sprint(findings), Risk: risk},
		{Label: "Devices", Count: fmt.Sprint(len(alive(st.g)))},
	}
	if st.drift != nil {
		items = append(items, kit.Tab{Label: "Drift", Count: fmt.Sprint(len(st.drift.Changes))})
	}
	if st.tab >= len(items) {
		st.tab = tabDevice
	}
	tabs := kit.NewTabs(items, func(i int) { st.tab = i; t.rebuildPanel() })
	tabs.Selected = st.tab

	var body fyne.CanvasObject
	switch st.tab {
	case tabFindings:
		body = t.findingsView()
	case tabDevices:
		body = t.devicesView()
	case tabDrift:
		body = t.driftView()
	default:
		body = t.deviceView()
	}
	scroll := container.NewVScroll(kit.Pad(26, 30, colPad, colPad, body))
	return kit.Width(rightWidth, container.NewBorder(kit.Pad(22, 0, colPad, colPad, tabs), nil, nil, nil, scroll))
}

// confWord is a confidence printed as a word. Asserted and observed are drawn
// in the text colour, the weaker ones in dim; the word is always there, so
// colour is never the only carrier.
func confWord(c model.Confidence) fyne.CanvasObject {
	p := shell.Current()
	col := p.Dim
	if c >= model.High {
		col = p.FG
	}
	return kit.Mono(c.Label(), 10.5, col)
}

func evidence(s string) fyne.CanvasObject {
	l := widget.NewLabel(s)
	l.TextStyle = fyne.TextStyle{Monospace: true}
	l.Wrapping = fyne.TextWrapWord
	l.Importance = widget.LowImportance
	l.SizeName = shell.SizeEvidence
	return kit.Flush(l)
}

// bestClaim is the claim behind an entity's best value for a key, so the
// panel can show the evidence for exactly the value it prints.
func bestClaim(e *model.Entity, key string) (model.Claim, bool) {
	v, _ := e.Best(key)
	if v == "" {
		return model.Claim{}, false
	}
	for _, c := range e.All(key) {
		if c.Value == v {
			return c, true
		}
	}
	return model.Claim{}, false
}

func (t *Tool) deviceView() fyne.CanvasObject {
	p := shell.Current()
	st := &t.st
	body := container.NewVBox()
	if st.selectNote != "" {
		n := kit.Wrapped(st.selectNote, true)
		n.SizeName = shell.SizeSmall
		body.Add(kit.Flush(n))
		body.Add(kit.Space(1, 14))
	}
	e := st.selected
	if e == nil {
		body.Add(kit.Flush(kit.Wrapped("Click a device on the map, or open the Devices tab.", true)))
		return body
	}
	addrs := strings.Join(e.Addresses(), ", ")
	if macs := e.MACs(); len(macs) > 0 {
		addrs += "   " + strings.Join(macs, ", ")
	}
	body.Add(kit.Caps("Selected", p.Red))
	body.Add(kit.Space(1, 6))
	body.Add(kit.Light(e.Label(), 26, p.FG))
	body.Add(kit.Space(1, 4))
	body.Add(kit.Mono(addrs, 11.5, p.Dim))
	body.Add(kit.Space(1, 22))

	claim := func(label, key string, always bool) {
		c, ok := bestClaim(e, key)
		if !ok && !always {
			return
		}
		body.Add(kit.Text(label, 11.5, p.Dim))
		if !ok {
			nl := kit.Text("not learned", 15, p.Dim)
			nl.TextStyle.Italic = true
			body.Add(nl)
		} else {
			body.Add(container.NewBorder(nil, nil, nil, container.NewCenter(confWord(c.Confidence)),
				kit.Text(c.Value, 15, p.FG)))
			body.Add(evidence(c.Source.Method + ": " + c.Source.Evidence))
		}
		body.Add(kit.Space(1, 12))
	}
	claim("Name", "hostname", true)
	claim("Label", "label", false)
	claim("Class", "class", true)
	claim("Vendor", "vendor", false)
	claim("Model", "model", false)
	claim("OS", "os", false)

	seen := map[string]bool{}
	var svcs []model.Claim
	for _, c := range e.All("service") {
		if !seen[c.Value] {
			seen[c.Value] = true
			svcs = append(svcs, c)
		}
	}
	if len(svcs) > 0 {
		body.Add(kit.Space(1, 10))
		body.Add(kit.Caps("Services", p.Dim))
		body.Add(kit.Space(1, 6))
		for _, c := range svcs {
			body.Add(container.NewBorder(nil, nil, nil, container.NewCenter(confWord(c.Confidence)), kit.Text(c.Value, 14, p.FG)))
			body.Add(kit.Space(1, 4))
		}
	}
	return body
}

func (t *Tool) findingsView() fyne.CanvasObject {
	p := shell.Current()
	st := &t.st
	if len(st.g.Findings) == 0 {
		return kit.Flush(kit.Wrapped("Nothing worth reporting.", true))
	}
	list := container.NewVBox()
	for _, f := range st.g.Findings {
		rule := p.Faint
		switch f.Severity {
		case "risk":
			rule = p.Red
		case "warn":
			rule = p.Warn
		}
		bar := canvas.NewRectangle(rule)
		bar.SetMinSize(fyne.NewSize(3, 1))
		title := kit.Wrapped(f.Title, false)
		detail := kit.Wrapped(f.Detail, true)
		detail.SizeName = shell.SizeSmall
		text := container.NewVBox(container.NewBorder(nil, nil, nil, container.NewCenter(confWord(f.Confidence)), kit.Flush(title)), kit.Flush(detail))
		row := container.NewBorder(nil, nil, container.NewHBox(bar, kit.Space(10, 1)), nil, text)
		var target *model.Entity
		if len(f.Entities) > 0 {
			target = st.g.Entity(model.Identity(f.Entities[0]))
		}
		list.Add(t.clickable(kit.Pad(12, 12, 0, 0, row), target))
		list.Add(kit.HLine())
	}
	return list
}

func (t *Tool) devicesView() fyne.CanvasObject {
	p := shell.Current()
	st := &t.st
	filter := widget.NewEntry()
	filter.SetPlaceHolder("Filter by name, address or kind")
	filter.SetText(st.filter)
	rows := container.NewVBox()
	paint := func() {
		rows.Objects = nil
		q := strings.ToLower(st.filter)
		for _, e := range alive(st.g) {
			v, _ := e.Best("vendor")
			hay := strings.ToLower(strings.Join([]string{e.Label(), strings.Join(e.Addresses(), " "), v, sublabel(e)}, " "))
			if q != "" && !strings.Contains(hay, q) {
				continue
			}
			nameCol := p.FG
			switch {
			case e == st.selected:
				nameCol = p.Red
			case kindOf(e) == kindUnknown:
				nameCol = p.Dim
			}
			what := container.NewVBox()
			if a := strings.Join(e.Addresses(), " "); a != e.Label() {
				what.Add(container.NewHBox(layout.NewSpacer(), kit.Mono(a, 11, p.Dim)))
			}
			what.Add(container.NewHBox(layout.NewSpacer(), kit.Mono(sublabel(e), 11, p.Dim)))
			row := container.NewBorder(nil, nil, container.NewCenter(kit.Text(e.Label(), 14, nameCol)), nil, what)
			rows.Add(t.clickable(kit.Pad(10, 10, 0, 0, row), e))
			rows.Add(kit.HLine())
		}
		if len(rows.Objects) == 0 {
			rows.Add(kit.Pad(12, 0, 0, 0, kit.Flush(kit.Wrapped("No device matches.", true))))
		}
		rows.Refresh()
	}
	filter.OnChanged = func(s string) { st.filter = s; paint() }
	paint()
	return container.NewVBox(filter, kit.Space(1, 6), rows)
}

func (t *Tool) driftView() fyne.CanvasObject {
	p := shell.Current()
	st := &t.st
	r := st.drift
	body := container.NewVBox(kit.Text("Since the survey of "+st.earlier.StartedAt.Local().Format("2 Jan 2006, 15:04"), 13, p.Dim))
	for _, c := range r.Caveats {
		w := kit.Wrapped(c, false)
		w.Importance = widget.WarningImportance
		body.Add(kit.Flush(w))
	}
	body.Add(kit.Space(1, 6))
	for _, c := range r.Changes {
		block := container.NewVBox(kit.Caps(c.Kind, p.Dim), kit.Text(c.Entity, 14, p.FG))
		if s := changeText(c); s != "" {
			block.Add(evidence(s))
		}
		for _, d := range c.Details {
			block.Add(evidence(d))
		}
		if c.MatchedBy != "" {
			how := "matched by " + c.MatchedBy + ", " + c.Match.Label()
			if c.MatchedBy == "ip" {
				how = "matched by address only, " + c.Match.Label()
			}
			block.Add(kit.Mono(how, 10.5, p.Dim))
		}
		var target *model.Entity
		for _, e := range alive(st.g) {
			if e.Label() == c.Entity {
				target = e
			}
		}
		body.Add(t.clickable(kit.Pad(12, 12, 0, 0, block), target))
		body.Add(kit.HLine())
	}
	return body
}

// changeText says a change in words: an appeared device has no before, and a
// vanished one no after.
func changeText(c drift.Change) string {
	switch {
	case c.Was == "" && c.Now == "":
		return ""
	case c.Was == "":
		return c.Now
	case c.Now == "":
		return "was " + c.Was
	}
	return "was " + c.Was + ", now " + c.Now
}

// ---------- clickable rows ----------

type clickable struct {
	widget.BaseWidget
	content fyne.CanvasObject
	tapped  func()
}

func (c *clickable) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(c.content) }
func (c *clickable) Tapped(*fyne.PointEvent) {
	if c.tapped != nil {
		c.tapped()
	}
}
func (c *clickable) Cursor() desktop.Cursor {
	if c.tapped == nil {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

// clickable makes a row open its device. A row about no device (a finding
// about the whole network) is not clickable at all.
func (t *Tool) clickable(content fyne.CanvasObject, e *model.Entity) fyne.CanvasObject {
	c := &clickable{content: content}
	if e != nil {
		c.tapped = func() { t.selectEntity(e) }
	}
	c.ExtendBaseWidget(c)
	return c
}
