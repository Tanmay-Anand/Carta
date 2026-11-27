package survey

import (
	"image/color"
	"math"
	"slices"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"github.com/Tanmay-Anand/carta/desktop/shell"
	"github.com/Tanmay-Anand/carta/internal/model"
)

type mapNode struct {
	e      *model.Entity
	label  string
	kind   kind
	x, y   float64 // model space
	vx, vy float64
	r      float64
}

type mapLink struct{ a, b *mapNode }

// MapView draws a survey as claims: every device that answered and the
// gateway's links, with how much is known about each one carried by shape.
// Identified devices are filled, partly known ones are rings, unidentified
// ones are dashed amber rings, infrastructure is larger, and the selected
// device is red with a ring of its own (docs/DESIGN_SYSTEM_CARTA.md §4).
type MapView struct {
	widget.BaseWidget

	nodes []*mapNode
	links []*mapLink

	k, ox, oy float64 // view: screen = model*k + o
	fitted    bool
	fitSize   fyne.Size
	userMoved bool // once someone pans or zooms, a resize must not undo it

	selected *mapNode
	hovered  *mapNode
	// Dimmed draws the map at reduced strength while a survey runs: it is the
	// previous survey, still readable, not the one being taken.
	Dimmed bool

	drag     *mapNode
	panning  bool
	dragSeen bool

	// OnSelect is called when a device is clicked.
	OnSelect func(*model.Entity)
}

func NewMapView(g *model.Graph) *MapView {
	m := &MapView{}
	m.ExtendBaseWidget(m)
	m.SetGraph(g)
	return m
}

// SetGraph replaces the survey shown and lays it out afresh.
func (m *MapView) SetGraph(g *model.Graph) {
	m.nodes, m.links, m.selected, m.hovered = nil, nil, nil, nil
	m.fitted, m.userMoved = false, false
	es := alive(g)
	index := map[string]*mapNode{}
	for i, e := range es {
		// Seeded on a circle rather than at random: the same survey must give
		// the same picture, or two screenshots look like two networks.
		a := float64(i) / float64(len(es)) * 2 * math.Pi
		n := &mapNode{e: e, label: e.Label(), kind: kindOf(e), x: math.Cos(a) * 220, y: math.Sin(a) * 220, r: 4.5}
		if n.kind == kindInfra {
			n.r = 7
		}
		m.nodes = append(m.nodes, n)
		index[e.ID] = n
	}
	if g != nil {
		for _, ed := range g.Edges {
			if ed.Kind != model.EdgeGateway {
				continue
			}
			if a, b := index[ed.From], index[ed.To]; a != nil && b != nil && a != b {
				m.links = append(m.links, &mapLink{a: a, b: b})
			}
		}
	}
	m.layout()
	m.Refresh()
}

// Select marks a device as selected, as if it had been clicked.
func (m *MapView) Select(e *model.Entity) {
	m.selected = nil
	for _, n := range m.nodes {
		if n.e == e {
			m.selected = n
		}
	}
	m.Refresh()
}

// Fit frames the whole map again, undoing any pan or zoom.
func (m *MapView) Fit() {
	m.userMoved = false
	m.fit(m.Size())
	m.Refresh()
}

// layout is the web UI's spring-and-repulsion, run to a fixed count up front
// rather than animated: a map that keeps drifting cannot be pointed at.
func (m *MapView) layout() {
	const iterations = 400
	for step := 0; step < iterations; step++ {
		t := 1 - float64(step)/iterations
		for _, n := range m.nodes {
			for _, o := range m.nodes {
				if n == o {
					continue
				}
				dx, dy := n.x-o.x, n.y-o.y
				d2 := dx*dx + dy*dy
				if d2 == 0 {
					d2 = 0.01
				}
				f := 2400 / d2
				n.vx += dx * f * 0.02
				n.vy += dy * f * 0.02
			}
			n.vx -= n.x * 0.002
			n.vy -= n.y * 0.002
		}
		for _, l := range m.links {
			dx, dy := l.b.x-l.a.x, l.b.y-l.a.y
			d := math.Hypot(dx, dy)
			if d == 0 {
				d = 0.01
			}
			f := (d - 130) * 0.01
			l.a.vx += dx / d * f
			l.a.vy += dy / d * f
			l.b.vx -= dx / d * f
			l.b.vy -= dy / d * f
		}
		for _, n := range m.nodes {
			n.x += n.vx * t
			n.y += n.vy * t
			n.vx *= 0.82
			n.vy *= 0.82
		}
	}
}

// fit frames the map, leaving room on the right where labels run. It refuses
// a zero size, which would give a scale of zero: every node present and a
// blank map.
func (m *MapView) fit(size fyne.Size) {
	w, h := float64(size.Width), float64(size.Height)
	if len(m.nodes) == 0 || w < 2 || h < 2 {
		return
	}
	minX, maxX, minY, maxY := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
	for _, n := range m.nodes {
		minX, maxX = math.Min(minX, n.x), math.Max(maxX, n.x)
		minY, maxY = math.Min(minY, n.y), math.Max(maxY, n.y)
	}
	const left, right, top, bottom = 60, 190, 70, 80
	k := math.Min((w-left-right)/math.Max(maxX-minX, 1), (h-top-bottom)/math.Max(maxY-minY, 1))
	k = math.Min(k, 1.8)
	if k <= 0 || math.IsInf(k, 0) || math.IsNaN(k) {
		return
	}
	m.k = k
	m.ox = left + ((w-left-right)-(maxX-minX)*k)/2 - minX*k
	m.oy = top + ((h-top-bottom)-(maxY-minY)*k)/2 - minY*k
	m.fitted, m.fitSize = true, size
}

func (m *MapView) screen(n *mapNode) fyne.Position {
	return fyne.NewPos(float32(n.x*m.k+m.ox), float32(n.y*m.k+m.oy))
}

func (m *MapView) hit(p fyne.Position) *mapNode {
	for i := len(m.nodes) - 1; i >= 0; i-- {
		s := m.screen(m.nodes[i])
		if math.Hypot(float64(s.X-p.X), float64(s.Y-p.Y)) < m.nodes[i].r+10 {
			return m.nodes[i]
		}
	}
	return nil
}

func (m *MapView) Tapped(ev *fyne.PointEvent) {
	if n := m.hit(ev.Position); n != nil {
		m.selected = n
		m.Refresh()
		if m.OnSelect != nil {
			m.OnSelect(n.e)
		}
	}
}

func (m *MapView) MouseIn(*desktop.MouseEvent) {}
func (m *MapView) MouseOut()                   { m.hovered = nil; m.Refresh() }
func (m *MapView) MouseMoved(ev *desktop.MouseEvent) {
	if n := m.hit(ev.Position); n != m.hovered {
		m.hovered = n
		m.Refresh()
	}
}

// Cursor is a pointer over a device, so it reads as something to click.
func (m *MapView) Cursor() desktop.Cursor {
	if m.hovered != nil {
		return desktop.PointerCursor
	}
	return desktop.DefaultCursor
}

// Dragged pans the map. A drag that starts on a device moves the device.
func (m *MapView) Dragged(ev *fyne.DragEvent) {
	if !m.dragSeen {
		m.dragSeen = true
		m.drag = m.hit(ev.Position.Subtract(ev.Dragged))
		m.panning = m.drag == nil
	}
	if m.drag != nil {
		m.drag.x += float64(ev.Dragged.DX) / m.k
		m.drag.y += float64(ev.Dragged.DY) / m.k
	} else if m.panning {
		m.userMoved = true
		m.ox += float64(ev.Dragged.DX)
		m.oy += float64(ev.Dragged.DY)
	}
	m.Refresh()
}

func (m *MapView) DragEnd() { m.drag, m.panning, m.dragSeen = nil, false, false }

// Scrolled zooms about the pointer, so the device under it stays under it.
func (m *MapView) Scrolled(ev *fyne.ScrollEvent) {
	f := math.Pow(1.1, float64(ev.Scrolled.DY)/10)
	k := math.Max(0.3, math.Min(5, m.k*f))
	px, py := float64(ev.Position.X), float64(ev.Position.Y)
	m.ox = px - (px-m.ox)*k/m.k
	m.oy = py - (py-m.oy)*k/m.k
	m.k = k
	m.userMoved = true
	m.Refresh()
}

func (m *MapView) CreateRenderer() fyne.WidgetRenderer { return &mapRenderer{m: m} }

type mapRenderer struct {
	m       *MapView
	size    fyne.Size
	objects []fyne.CanvasObject
}

func (r *mapRenderer) Layout(size fyne.Size) {
	r.size = size
	// The window lays the map out more than once while it opens, first at a
	// token size. It re-frames on every resize until someone has moved it.
	if !r.m.fitted || (!r.m.userMoved && size != r.m.fitSize) {
		r.m.fit(size)
	}
	r.build()
}

func (r *mapRenderer) MinSize() fyne.Size           { return fyne.NewSize(240, 240) }
func (r *mapRenderer) Refresh()                     { r.build(); canvas.Refresh(r.m) }
func (r *mapRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *mapRenderer) Destroy()                     {}

type rect struct{ x0, y0, x1, y1 float32 }

func (a rect) overlaps(b rect) bool { return a.x0 < b.x1 && b.x0 < a.x1 && a.y0 < b.y1 && b.y0 < a.y1 }

// build redraws everything. A survey is tens of devices, not thousands, so
// rebuilding on each change is simpler than tracking what moved.
func (r *mapRenderer) build() {
	m := r.m
	p := shell.Current()
	fade := func(c color.Color) color.Color {
		if !m.Dimmed {
			return c
		}
		n := color.NRGBAModel.Convert(c).(color.NRGBA)
		n.A = uint8(float64(n.A) * 0.45)
		return n
	}
	objs := []fyne.CanvasObject{}

	for _, l := range m.links {
		ln := canvas.NewLine(fade(p.Thread))
		ln.StrokeWidth = 1
		ln.Position1, ln.Position2 = m.screen(l.a), m.screen(l.b)
		objs = append(objs, ln)
	}

	// Devices first, labels after, so the label placement can avoid dots.
	var taken []rect
	for _, n := range m.nodes {
		c := m.screen(n)
		rad := float32(n.r)
		taken = append(taken, rect{c.X - rad - 3, c.Y - rad - 3, c.X + rad + 3, c.Y + rad + 3})
		sel := n == m.selected
		col := p.FG
		if sel {
			col = p.Red
		}
		if sel {
			objs = append(objs, ring(c, rad+9, fade(p.Red), 1))
		}
		switch n.kind {
		case kindUnknown:
			if !sel {
				col = p.Amber
			}
			objs = append(objs, dashedRing(c, rad, fade(col), 1.3)...)
		case kindPartial:
			objs = append(objs, ring(c, rad, fade(col), 1.3))
		default:
			objs = append(objs, disc(c, rad, fade(col)))
		}
	}

	// Labels: right of the dot, else left, below or above. A label that collides
	// everywhere is shown only when its device is hovered or selected, so the
	// map never prints text over text (docs/DESIGN_SYSTEM_CARTA.md §4).
	// The selected and hovered labels are placed first, so they always show.
	var order []*mapNode
	for _, first := range []*mapNode{m.selected, m.hovered} {
		if first != nil && !slices.Contains(order, first) {
			order = append(order, first)
		}
	}
	for _, n := range m.nodes {
		if !slices.Contains(order, n) {
			order = append(order, n)
		}
	}
	for _, n := range order {
		c := m.screen(n)
		rad := float32(n.r)
		sel, hov := n == m.selected, n == m.hovered
		col := p.Dim
		if sel || hov {
			col = p.FG
		}
		t := canvas.NewText(n.label, fade(col))
		t.TextSize = 12
		sz := t.MinSize()
		gap := rad + 8
		if sel {
			gap = rad + 14
		}
		// Centred below or above, slid sideways to stay inside the map, so a
		// device near the edge still gets its name.
		centred := min(max(c.X-sz.Width/2, 6), r.size.Width-6-sz.Width)
		cands := []fyne.Position{
			{X: c.X + gap, Y: c.Y - sz.Height/2},
			{X: c.X - gap - sz.Width, Y: c.Y - sz.Height/2},
			{X: centred, Y: c.Y + gap - 2},
			{X: centred, Y: c.Y - gap - sz.Height + 2},
		}
		for _, at := range cands {
			box := rect{at.X - 2, at.Y + 2, at.X + sz.Width + 2, at.Y + sz.Height - 2}
			inside := box.x0 >= 4 && box.x1 <= r.size.Width-4 && box.y0 >= 4 && box.y1 <= r.size.Height-4
			clear := true
			for _, o := range taken {
				if box.overlaps(o) {
					clear = false
					break
				}
			}
			if (inside && clear) || ((sel || hov) && at == cands[len(cands)-1]) {
				t.Move(at)
				t.Resize(sz)
				objs = append(objs, t)
				taken = append(taken, box)
				break
			}
		}
	}
	r.objects = objs
}

func disc(c fyne.Position, r float32, fill color.Color) *canvas.Circle {
	o := canvas.NewCircle(fill)
	o.Position1 = fyne.NewPos(c.X-r, c.Y-r)
	o.Position2 = fyne.NewPos(c.X+r, c.Y+r)
	return o
}

func ring(c fyne.Position, r float32, stroke color.Color, width float32) *canvas.Circle {
	o := canvas.NewCircle(color.Transparent)
	o.StrokeColor, o.StrokeWidth = stroke, width
	o.Position1 = fyne.NewPos(c.X-r, c.Y-r)
	o.Position2 = fyne.NewPos(c.X+r, c.Y+r)
	return o
}

// dashedRing approximates a dashed outline with short chords; Fyne's circle
// has no dash style.
func dashedRing(c fyne.Position, r float32, col color.Color, width float32) []fyne.CanvasObject {
	const segments = 12
	var out []fyne.CanvasObject
	for i := 0; i < segments; i += 2 {
		a0 := float64(i) / segments * 2 * math.Pi
		a1 := float64(i+1) / segments * 2 * math.Pi
		l := canvas.NewLine(col)
		l.StrokeWidth = width
		l.Position1 = fyne.NewPos(c.X+r*float32(math.Cos(a0)), c.Y+r*float32(math.Sin(a0)))
		l.Position2 = fyne.NewPos(c.X+r*float32(math.Cos(a1)), c.Y+r*float32(math.Sin(a1)))
		out = append(out, l)
	}
	return out
}
