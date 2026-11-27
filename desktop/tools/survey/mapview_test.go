package survey

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
)

// At the map's size in the default window, every device gets its name, no two
// names overlap, and none runs off the map.
func TestEveryMapLabelShowsWithoutOverlap(t *testing.T) {
	test.NewTempApp(t)
	m := NewMapView(Synthetic())
	size := fyne.NewSize(518, 683)
	r := test.TempWidgetRenderer(t, m).(*mapRenderer)
	r.Layout(size)

	var boxes []rect
	labels := map[string]bool{}
	for _, o := range r.objects {
		tx, ok := o.(*canvas.Text)
		if !ok {
			continue
		}
		p, s := tx.Position(), tx.MinSize()
		b := rect{p.X, p.Y + 2, p.X + s.Width, p.Y + s.Height - 2}
		if b.x0 < 0 || b.y0 < 0 || b.x1 > size.Width || b.y1 > size.Height {
			t.Errorf("%q runs off the map at %v", tx.Text, p)
		}
		for _, o := range boxes {
			if b.overlaps(o) {
				t.Errorf("%q overlaps another label", tx.Text)
			}
		}
		boxes = append(boxes, b)
		labels[tx.Text] = true
	}
	for _, n := range m.nodes {
		if !labels[n.label] {
			t.Errorf("%s has no label", n.label)
		}
	}
}

// A selected device's label is always shown, even where it has to overlap.
func TestTheSelectedLabelAlwaysShows(t *testing.T) {
	test.NewTempApp(t)
	m := NewMapView(Synthetic())
	r := test.TempWidgetRenderer(t, m).(*mapRenderer)
	r.Layout(fyne.NewSize(240, 240)) // cramped on purpose
	for _, n := range m.nodes {
		m.Select(n.e)
		r.Layout(fyne.NewSize(240, 240))
		found := false
		for _, o := range r.objects {
			if tx, ok := o.(*canvas.Text); ok && tx.Text == n.label {
				found = true
			}
		}
		if !found {
			t.Errorf("selected %s but its label is hidden", n.label)
		}
	}
}
