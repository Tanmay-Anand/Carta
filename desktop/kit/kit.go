// Package kit holds the few controls design D needs that Fyne does not have:
// a text button, the Survey pill, underline tabs, the coverage dot matrix, a
// 2px progress line, hairlines and a tag. Each reads its colours from the
// palette when it draws, so a light/dark switch needs only a refresh.
package kit

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/Tanmay-Anand/carta/desktop/shell"
)

// ---------- text ----------

// Text is a single line in Inter Regular.
func Text(s string, size float32, c color.Color) *canvas.Text {
	t := canvas.NewText(s, c)
	t.TextSize = size
	return t
}

// Light is a single line in Inter Light, for the headline.
func Light(s string, size float32, c color.Color) *canvas.Text {
	t := Text(s, size, c)
	t.FontSource = shell.SansLight
	return t
}

// Mono is a single line of measured data in JetBrains Mono.
func Mono(s string, size float32, c color.Color) *canvas.Text {
	t := Text(s, size, c)
	t.TextStyle = fyne.TextStyle{Monospace: true}
	return t
}

// Caps is a small uppercase label ("COVERAGE"). Fyne text has no letter
// spacing, so the tracking the design asks for is left out rather than faked.
func Caps(s string, c color.Color) *canvas.Text {
	return Mono(strings.ToUpper(s), 10.5, c)
}

// Flush cancels the padding a Label puts around its text, so wrapped prose
// lines up with the canvas text above and below it.
func Flush(l *widget.Label) fyne.CanvasObject { return Pad(-6, -6, -8, -8, l) }

// Wrapped is prose that wraps to its container's width. Place it with Flush.
func Wrapped(s string, dim bool) *widget.Label {
	l := widget.NewLabel(s)
	l.Wrapping = fyne.TextWrapWord
	if dim {
		l.Importance = widget.LowImportance
	}
	return l
}

// ---------- layout ----------

// Pad surrounds an object with fixed space.
func Pad(top, bottom, left, right float32, o fyne.CanvasObject) fyne.CanvasObject {
	return container.New(layout.NewCustomPaddedLayout(top, bottom, left, right), o)
}

type fixedWidth struct{ w float32 }

func (f fixedWidth) MinSize(objs []fyne.CanvasObject) fyne.Size {
	h := float32(0)
	for _, o := range objs {
		h = max(h, o.MinSize().Height)
	}
	return fyne.NewSize(f.w, h)
}

func (f fixedWidth) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(size)
	}
}

// Width gives an object a fixed width, as a side column in a Border.
func Width(w float32, o fyne.CanvasObject) fyne.CanvasObject {
	return container.New(fixedWidth{w}, o)
}

type gap struct{ w, h float32 }

func (g gap) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(g.w, g.h) }
func (g gap) Layout([]fyne.CanvasObject, fyne.Size)  {}

// Space is fixed empty space.
func Space(w, h float32) fyne.CanvasObject { return container.New(gap{w, h}) }

// HLine and VLine are hairlines in the Hair token.
func HLine() fyne.CanvasObject { return line(true) }
func VLine() fyne.CanvasObject { return line(false) }

func line(horizontal bool) fyne.CanvasObject {
	r := canvas.NewRectangle(shell.Current().Hair)
	if horizontal {
		r.SetMinSize(fyne.NewSize(1, 1))
	} else {
		r.SetMinSize(fyne.NewSize(1, 1))
	}
	return r
}

// Tag is a mono uppercase word in a hairline box, in Warn: STOPPED EARLY.
func Tag(s string) fyne.CanvasObject {
	p := shell.Current()
	box := canvas.NewRectangle(color.Transparent)
	box.StrokeColor, box.StrokeWidth = p.Warn, 1
	return container.NewCenter(container.NewStack(box, Pad(2, 2, 6, 6, Mono(strings.ToUpper(s), 9.5, p.Warn))))
}

// ---------- TextButton ----------

// TextButton is a word that acts: dim at rest, the text colour on hover.
// "Open", "Compare", "Survey quietly", "Fit".
type TextButton struct {
	widget.BaseWidget
	Label    string
	TextSize float32
	Active   bool // drawn in the text colour at rest, as a selected tool is
	OnTapped func()
	disabled bool
	hovered  bool
	text     *canvas.Text
}

func NewTextButton(label string, size float32, tapped func()) *TextButton {
	b := &TextButton{Label: label, TextSize: size, OnTapped: tapped}
	b.ExtendBaseWidget(b)
	return b
}

func (b *TextButton) Disable() { b.disabled = true; b.Refresh() }
func (b *TextButton) Enable()  { b.disabled = false; b.Refresh() }

func (b *TextButton) Tapped(*fyne.PointEvent) {
	if !b.disabled && b.OnTapped != nil {
		b.OnTapped()
	}
}
func (b *TextButton) MouseIn(*desktop.MouseEvent)    { b.hovered = true; b.Refresh() }
func (b *TextButton) MouseMoved(*desktop.MouseEvent) {}
func (b *TextButton) MouseOut()                      { b.hovered = false; b.Refresh() }
func (b *TextButton) Cursor() desktop.Cursor {
	if b.disabled {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

func (b *TextButton) CreateRenderer() fyne.WidgetRenderer {
	b.text = Text(b.Label, b.TextSize, nil)
	r := &simpleRenderer{objects: []fyne.CanvasObject{b.text}}
	r.refresh = func() {
		p := shell.Current()
		b.text.Text, b.text.TextSize = b.Label, b.TextSize
		switch {
		case b.disabled:
			b.text.Color = p.Faint
		case b.hovered || b.Active:
			b.text.Color = p.FG
		default:
			b.text.Color = p.Dim
		}
		b.text.Refresh()
	}
	r.layout = func(size fyne.Size) { b.text.Move(fyne.NewPos(0, (size.Height-b.text.MinSize().Height)/2)) }
	r.min = func() fyne.Size { return b.text.MinSize() }
	r.refresh()
	return r
}

// ---------- Pill ----------

// Pill is the one strong control on a screen: Survey, or Stop in red. A
// rounded hairline at rest, filled on hover.
type Pill struct {
	widget.BaseWidget
	Label    string
	Danger   bool
	OnTapped func()
	hovered  bool
}

func NewPill(label string, danger bool, tapped func()) *Pill {
	p := &Pill{Label: label, Danger: danger, OnTapped: tapped}
	p.ExtendBaseWidget(p)
	return p
}

func (p *Pill) Tapped(*fyne.PointEvent) {
	if p.OnTapped != nil {
		p.OnTapped()
	}
}
func (p *Pill) MouseIn(*desktop.MouseEvent)    { p.hovered = true; p.Refresh() }
func (p *Pill) MouseMoved(*desktop.MouseEvent) {}
func (p *Pill) MouseOut()                      { p.hovered = false; p.Refresh() }
func (p *Pill) Cursor() desktop.Cursor         { return desktop.PointerCursor }

func (p *Pill) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	bg.StrokeWidth = 1
	text := Text(p.Label, 14, nil)
	r := &simpleRenderer{objects: []fyne.CanvasObject{bg, text}}
	r.refresh = func() {
		pal := shell.Current()
		c := pal.FG
		if p.Danger {
			c = pal.Red
		}
		text.Text = p.Label
		bg.StrokeColor = c
		if p.hovered {
			bg.FillColor, text.Color = c, pal.BG
		} else {
			bg.FillColor, text.Color = color.Transparent, c
		}
		bg.Refresh()
		text.Refresh()
	}
	r.min = func() fyne.Size { return text.MinSize().Add(fyne.NewSize(68, 22)) }
	r.layout = func(size fyne.Size) {
		bg.Resize(size)
		bg.CornerRadius = size.Height / 2
		ts := text.MinSize()
		text.Move(fyne.NewPos((size.Width-ts.Width)/2, (size.Height-ts.Height)/2))
	}
	r.refresh()
	return r
}

// ---------- Tabs ----------

// Tab is one tab's label, with an optional count shown small beside it.
type Tab struct {
	Label string
	Count string
	Risk  bool // the count is drawn in red
}

// Tabs is a row of words with a hairline under the row and a text-coloured
// underline under the selected word.
type Tabs struct {
	widget.BaseWidget
	Items    []Tab
	Selected int
	OnSelect func(int)
	spans    []float32 // x start and end of each tab, from the last layout
}

func NewTabs(items []Tab, selected func(int)) *Tabs {
	t := &Tabs{Items: items, OnSelect: selected}
	t.ExtendBaseWidget(t)
	return t
}

// Select shows tab i as selected without calling OnSelect.
func (t *Tabs) Select(i int) { t.Selected = i; t.Refresh() }

func (t *Tabs) Tapped(ev *fyne.PointEvent) {
	for i := 0; i+1 < len(t.spans); i += 2 {
		if ev.Position.X >= t.spans[i]-8 && ev.Position.X <= t.spans[i+1]+8 {
			t.Selected = i / 2
			t.Refresh()
			if t.OnSelect != nil {
				t.OnSelect(t.Selected)
			}
			return
		}
	}
}
func (t *Tabs) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (t *Tabs) CreateRenderer() fyne.WidgetRenderer {
	r := &simpleRenderer{}
	const gapX, bottomPad = 24, 12
	r.refresh = func() {
		p := shell.Current()
		r.objects = nil
		for i, it := range t.Items {
			c := p.Dim
			if i == t.Selected {
				c = p.FG
			}
			r.objects = append(r.objects, Text(it.Label, 14, c))
			cc := p.Dim
			if it.Risk {
				cc = p.Red
			}
			r.objects = append(r.objects, Mono(it.Count, 9.5, cc))
		}
		base := canvas.NewRectangle(p.Hair)
		under := canvas.NewRectangle(p.FG)
		r.objects = append(r.objects, base, under)
		if r.size.Width > 0 {
			r.layout(r.size)
		}
	}
	r.min = func() fyne.Size {
		w, h := float32(0), float32(0)
		for i := 0; i+1 < len(r.objects)-2; i += 2 {
			l, c := r.objects[i].MinSize(), r.objects[i+1].MinSize()
			w += l.Width + c.Width + 3 + gapX
			h = max(h, l.Height)
		}
		return fyne.NewSize(w, h+bottomPad+1)
	}
	r.layout = func(size fyne.Size) {
		r.size = size
		x := float32(0)
		t.spans = t.spans[:0]
		n := len(r.objects) - 2
		for i := 0; i+1 < n; i += 2 {
			label, count := r.objects[i], r.objects[i+1]
			ls, cs := label.MinSize(), count.MinSize()
			label.Move(fyne.NewPos(x, 0))
			label.Resize(ls)
			count.Move(fyne.NewPos(x+ls.Width+3, -2))
			count.Resize(cs)
			end := x + ls.Width + 3 + cs.Width
			t.spans = append(t.spans, x, end)
			if i/2 == t.Selected {
				u := r.objects[n+1]
				u.Move(fyne.NewPos(x, size.Height-1))
				u.Resize(fyne.NewSize(ls.Width, 1))
			}
			x = end + gapX
		}
		base := r.objects[n]
		base.Move(fyne.NewPos(-28, size.Height-1))
		base.Resize(fyne.NewSize(size.Width+56, 1))
	}
	r.refresh()
	return r
}

// ---------- DotMatrix ----------

// Dot is the state of one address in the coverage matrix.
type Dot uint8

const (
	Unreached Dot = iota // never asked: a ring
	Silent               // asked, did not answer: a faint dot
	Answered             // answered: a text-coloured dot
)

// DotMatrix draws one dot per address, in order, so how much of the network
// answered, stayed silent or was never asked is seen at once.
type DotMatrix struct {
	widget.BaseWidget
	Dots    []Dot
	Columns int
	Width   float32 // the width the matrix is laid out for, so MinSize can give a height
}

func NewDotMatrix(width float32) *DotMatrix {
	m := &DotMatrix{Columns: 24, Width: width}
	m.ExtendBaseWidget(m)
	return m
}

func (m *DotMatrix) geometry() (cols int, dot, gap float32) {
	cols = m.Columns
	if len(m.Dots) > 256 {
		cols = 32
	}
	if len(m.Dots) > 512 {
		cols = 48
	}
	gap = 4
	if cols > 24 {
		gap = 3
	}
	dot = (m.Width - gap*float32(cols-1)) / float32(cols)
	return cols, dot, gap
}

func (m *DotMatrix) CreateRenderer() fyne.WidgetRenderer {
	r := &simpleRenderer{}
	r.refresh = func() {
		p := shell.Current()
		cols, dot, gap := m.geometry()
		r.objects = r.objects[:0]
		for i, d := range m.Dots {
			c := canvas.NewCircle(color.Transparent)
			switch d {
			case Answered:
				c.FillColor = p.FG
			case Silent:
				c.FillColor = p.Faint
			default:
				// A ring in Dim, not Faint: "never asked" is a meaning, and a
				// meaning must clear 3:1.
				c.StrokeColor, c.StrokeWidth = p.Dim, 1
			}
			x := float32(i%cols) * (dot + gap)
			y := float32(i/cols) * (dot + gap)
			inset := float32(0)
			if d == Unreached {
				inset = 0.5
			}
			c.Position1 = fyne.NewPos(x+inset, y+inset)
			c.Position2 = fyne.NewPos(x+dot-inset, y+dot-inset)
			r.objects = append(r.objects, c)
		}
	}
	r.min = func() fyne.Size {
		cols, dot, gap := m.geometry()
		rows := (len(m.Dots) + cols - 1) / cols
		return fyne.NewSize(m.Width, float32(rows)*(dot+gap)-gap)
	}
	r.layout = func(fyne.Size) {}
	r.refresh()
	return r
}

// ---------- ProgressLine ----------

// ProgressLine is a 2px line: done in the text colour over a hairline.
type ProgressLine struct {
	widget.BaseWidget
	Value float64 // 0..1
}

func NewProgressLine() *ProgressLine {
	p := &ProgressLine{}
	p.ExtendBaseWidget(p)
	return p
}

func (p *ProgressLine) SetValue(v float64) { p.Value = v; p.Refresh() }

func (p *ProgressLine) CreateRenderer() fyne.WidgetRenderer {
	track, done := canvas.NewRectangle(nil), canvas.NewRectangle(nil)
	r := &simpleRenderer{objects: []fyne.CanvasObject{track, done}}
	r.refresh = func() {
		pal := shell.Current()
		track.FillColor, done.FillColor = pal.Hair, pal.FG
		if r.size.Width > 0 {
			r.layout(r.size)
		}
		track.Refresh()
		done.Refresh()
	}
	r.min = func() fyne.Size { return fyne.NewSize(10, 2) }
	r.layout = func(size fyne.Size) {
		r.size = size
		track.Resize(size)
		done.Resize(fyne.NewSize(size.Width*float32(max(0, min(1, p.Value))), size.Height))
	}
	r.refresh()
	return r
}

// ---------- shared renderer ----------

// simpleRenderer is a renderer assembled from closures, enough for widgets
// that draw a handful of canvas objects.
type simpleRenderer struct {
	objects []fyne.CanvasObject
	size    fyne.Size
	refresh func()
	layout  func(fyne.Size)
	min     func() fyne.Size
}

func (r *simpleRenderer) Layout(s fyne.Size) {
	r.size = s
	if r.layout != nil {
		r.layout(s)
	}
}
func (r *simpleRenderer) MinSize() fyne.Size {
	if r.min != nil {
		return r.min()
	}
	return fyne.Size{}
}
func (r *simpleRenderer) Refresh() {
	if r.refresh != nil {
		r.refresh()
	}
	if r.layout != nil && r.size.Width > 0 {
		r.layout(r.size)
	}
	for _, o := range r.objects {
		o.Refresh()
	}
}
func (r *simpleRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *simpleRenderer) Destroy()                     {}
