package survey

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"

	"github.com/Tanmay-Anand/carta/desktop/kit"
	"github.com/Tanmay-Anand/carta/desktop/shell"
	"github.com/Tanmay-Anand/carta/internal/model"
)

// comparePicker lists earlier surveys to compare the one on screen with.
//
// The operator picks; the app never chooses for them, because nothing
// identifies a network for certain: two homes on 192.168.1.0/24 look alike.
// Each row shows the few clues there are, and marks in Warn the ones that
// differ from the survey on screen. A difference is evidence of another
// network; a match proves nothing.
func (t *Tool) comparePicker(surveys []*model.Graph, choose func(*model.Graph)) fyne.CanvasObject {
	p := shell.Current()
	hint := kit.Wrapped("Pick a survey of the same network. Nothing here proves it is the same one: "+
		"two homes on 192.168.1.0/24 look alike. Values that differ from the survey on screen are marked.", true)
	hint.SizeName = shell.SizeSmall
	body := container.NewVBox(kit.Flush(hint), kit.Space(1, 10))

	if t.st.earlier != nil {
		body.Add(container.NewHBox(kit.NewTextButton("Clear the comparison", 13, func() { choose(nil) })))
		body.Add(kit.Space(1, 8))
	}
	if len(surveys) == 0 {
		body.Add(kit.Flush(kit.Wrapped("There is no earlier survey saved in "+t.dir+".", true)))
	}

	cur := t.st.g.Vantage
	field := func(label, value string, same bool) fyne.CanvasObject {
		c := p.Dim
		if !same {
			c = p.Warn
		}
		return kit.Mono(label+" "+value, 11, c)
	}
	for _, g := range surveys {
		v := g.Vantage
		head := container.NewHBox(kit.Text(g.StartedAt.Local().Format("2 Jan 2006, 15:04"), 14, p.FG))
		if g.Meta["cut_short"] != "" {
			head.Add(kit.Space(8, 1))
			head.Add(kit.Tag("Stopped early"))
		}
		if v.InstrumentName() != cur.InstrumentName() {
			head.Add(kit.Space(8, 1))
			head.Add(kit.Mono(strings.TrimPrefix(v.InstrumentName(), "carta-"), 10.5, p.Warn))
		}
		clues := container.NewHBox(
			field("net", v.Prefix, v.Prefix == cur.Prefix), kit.Space(14, 1),
			field("gw", v.Gateway, v.Gateway == cur.Gateway), kit.Space(14, 1),
			kit.Mono(fmt.Sprintf("%d devices", len(alive(g))), 11, p.Dim),
		)
		row := &clickable{content: kit.Pad(10, 10, 0, 0, container.NewVBox(head, clues))}
		g := g
		row.tapped = func() { choose(g) }
		row.ExtendBaseWidget(row)
		body.Add(row)
		body.Add(kit.HLine())
	}
	scroll := container.NewVScroll(body)
	scroll.SetMinSize(fyne.NewSize(500, 380))
	return scroll
}
