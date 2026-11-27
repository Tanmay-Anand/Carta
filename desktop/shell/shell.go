package shell

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
)

// Tool is one instrument in the window. Carta's survey is the first; a new
// tool is one package that implements this and one line in the registry.
type Tool interface {
	Name() string
	// View builds the tool's content once; the shell keeps it while another
	// tool is showing, so switching back loses nothing.
	View(w fyne.Window) fyne.CanvasObject
	// Stop ends anything the tool has running. Called when the window closes.
	Stop()
}

// WithActions is a tool that puts actions at the right of the top bar while
// it is showing ("Open", "Compare").
type WithActions interface {
	Actions() fyne.CanvasObject
}

// New builds the window: the top bar, a hairline, and the selected tool.
//
// toolButton draws one tool's name in the bar; it is passed in rather than
// built here so the shell does not depend on the kit that depends on it.
func New(w fyne.Window, tools []Tool, toolButton func(name string, active bool, tapped func()) fyne.CanvasObject) fyne.CanvasObject {
	p := Current()
	views := make([]fyne.CanvasObject, len(tools))
	for i, t := range tools {
		views[i] = t.View(w)
	}
	content := container.NewStack(views...)
	sep := func(w float32) fyne.CanvasObject {
		r := canvas.NewRectangle(nil)
		r.SetMinSize(fyne.NewSize(w, 1))
		return r
	}
	actions := container.NewStack()
	names := container.NewHBox()

	var show func(int)
	show = func(i int) {
		for j, v := range views {
			if j == i {
				v.Show()
			} else {
				v.Hide()
			}
		}
		names.Objects = nil
		for j, t := range tools {
			names.Add(toolButton(t.Name(), j == i, func() { show(j) }))
			names.Add(sep(22))
		}
		// Where the next tools will go, said plainly rather than left as a
		// mystery gap: the bar is meant to grow.
		later := canvas.NewText("More tools later", p.Dim)
		later.TextSize = 14
		names.Add(container.NewCenter(later))
		actions.Objects = nil
		if a, ok := tools[i].(WithActions); ok {
			actions.Objects = []fyne.CanvasObject{a.Actions()}
		}
		names.Refresh()
		actions.Refresh()
		content.Refresh()
	}
	show(0)

	brand := canvas.NewText("C A R T A", p.FG)
	brand.TextStyle = fyne.TextStyle{Monospace: true}
	brand.TextSize = 12

	row := container.NewHBox(container.NewCenter(brand), sep(26), names, layout.NewSpacer(), actions)
	bar := container.New(layout.NewCustomPaddedLayout(14, 14, 28, 28), row)
	hair := canvas.NewRectangle(p.Hair)
	hair.SetMinSize(fyne.NewSize(1, 1))
	return container.NewBorder(container.NewVBox(bar, hair), nil, nil, nil, content)
}
