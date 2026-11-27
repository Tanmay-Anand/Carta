// Package survey is Carta's survey as a desktop tool: the summary, the map,
// the panel, and the controls that run a survey (docs/DESIGN_SYSTEM_CARTA.md
// §3.1, mock desktop/mocks/d-combined.html).
package survey

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"

	"github.com/Tanmay-Anand/carta/desktop/kit"
	"github.com/Tanmay-Anand/carta/internal/drift"
	"github.com/Tanmay-Anand/carta/internal/model"
	"github.com/Tanmay-Anand/carta/internal/pack"
	"github.com/Tanmay-Anand/carta/internal/store/snapshot"
)

// Tool is the survey tool.
type Tool struct {
	st     state
	runner Runner
	// dir is where surveys are saved and where the last one is read from on
	// start. Empty in the demo, which saves nothing.
	dir string
	w   fyne.Window

	root    *fyne.Container // replaced wholesale by rebuild
	left    *fyne.Container // replaced on every progress tick
	right   *fyne.Container // replaced when the selection or tab changes
	mapView *MapView        // kept across rebuilds so a pan or zoom survives

	mu     sync.Mutex
	cancel context.CancelFunc
	// running counts survey goroutines, so closing the window can wait for a
	// save in progress rather than cut a file off half written.
	running sync.WaitGroup
	// closed is set when the window closes. After it, the survey's goroutine
	// must not hand the UI any more work: the event loop is shutting down.
	closed atomic.Bool

	// finished, when set, is told each time a survey ends; tests wait on it.
	finished chan struct{}

	// demoEarlier stands in for the saved surveys in the demo, which saves
	// nothing, so Compare has something to offer.
	demoEarlier []*model.Graph
}

// New is the survey tool proper: it surveys the network this computer is on,
// saves each survey into dir, and opens on the last one saved there.
func New(dir string) *Tool {
	return newTool(dir, realRunner{})
}

func newTool(dir string, r Runner) *Tool {
	t := &Tool{runner: r, dir: dir}
	if g, err := snapshot.Read(filepath.Join(dir, pack.BaselineName)); err == nil {
		t.show(g, nil)
		t.st.run.stoppedIn = g.Meta["cut_short"]
		t.selectGateway()
	}
	return t
}

// NewDemo shows the fictional 192.168.10.0/24 and surveys with a stand-in
// that sends nothing and saves nothing. It is for screenshots and trying the
// app: no real network belongs in either.
func NewDemo() *Tool {
	t := &Tool{runner: fakeRunner{step: 100 * time.Millisecond}, demoEarlier: []*model.Graph{SyntheticEarlier()}}
	t.show(SyntheticEarlier(), nil)
	t.show(Synthetic(), nil)
	t.selectGateway()
	return t
}

// selectGateway selects the gateway, so the panel has something to show
// before anyone has clicked.
func (t *Tool) selectGateway() {
	for _, e := range alive(t.st.g) {
		if kindOf(e) == kindInfra && e.Label() != t.st.g.Vantage.Host {
			t.st.selected = e
			break
		}
	}
	t.st.selectNote = ""
}

func (t *Tool) Name() string { return "Survey" }

// Stop ends a running survey because the window is closing. It differs from
// the Stop button (stop) in that nothing may be drawn afterwards, and in that
// it waits, for a few seconds at most, for the survey to finish saving.
func (t *Tool) Stop() {
	t.closed.Store(true)
	t.mu.Lock()
	if t.cancel != nil {
		t.cancel()
	}
	t.mu.Unlock()
	done := make(chan struct{})
	go func() { t.running.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

func (t *Tool) Actions() fyne.CanvasObject {
	return container.NewHBox(
		container.NewCenter(kit.NewTextButton("Open", 14, t.openDialog)),
		kit.Space(22, 1),
		container.NewCenter(kit.NewTextButton("Compare", 14, t.compareDialog)),
	)
}

func (t *Tool) View(w fyne.Window) fyne.CanvasObject {
	t.w = w
	t.mapView = NewMapView(t.st.g)
	t.mapView.Select(t.st.selected)
	t.mapView.OnSelect = t.selectEntity
	t.root = container.NewStack()
	t.rebuild()
	// A light/dark switch changes every colour, and the widgets read their
	// colours when they are built: rebuild them all.
	fyne.CurrentApp().Settings().AddListener(func(fyne.Settings) { fyne.Do(t.rebuild) })
	return t.root
}

func (t *Tool) rebuild() {
	t.left = container.NewStack(t.summary())
	t.right = container.NewStack(t.panel())
	t.root.Objects = []fyne.CanvasObject{container.NewBorder(nil, nil,
		container.NewHBox(t.left, kit.VLine()),
		container.NewHBox(kit.VLine(), t.right),
		t.mapArea())}
	t.root.Refresh()
}

func (t *Tool) rebuildLeft() {
	t.left.Objects = []fyne.CanvasObject{t.summary()}
	t.left.Refresh()
	t.mapView.Dimmed = t.st.run.running
	t.mapView.Refresh()
}

func (t *Tool) rebuildPanel() {
	t.right.Objects = []fyne.CanvasObject{t.panel()}
	t.right.Refresh()
}

func (t *Tool) selectEntity(e *model.Entity) {
	t.st.selected, t.st.selectNote, t.st.tab = e, "", tabDevice
	t.mapView.Select(e)
	t.rebuildPanel()
}

// show puts a survey on screen. The survey it replaces becomes the one it is
// compared with, and the selected device is looked for in the new survey by
// the rules drift uses, rather than by address.
func (t *Tool) show(g *model.Graph, quiet *bool) {
	st := &t.st
	st.earlier, st.drift = nil, nil
	if st.g != nil {
		st.earlier = st.g
		r := drift.Compare(st.earlier, g)
		st.drift = &r
	}
	st.g = g
	st.selectNote = ""
	if st.selected != nil {
		found, by, _ := drift.Find(st.selected, g)
		cut := g.Meta["cut_short"]
		switch {
		case found == nil && cut != "":
			st.selectNote = st.selected.Label() + " was not seen, but the survey stopped during " +
				strings.ToLower(stageName(cut)) + ", before it could look. Nothing is known about it from this survey."
		case found == nil && quiet != nil && *quiet:
			st.selectNote = st.selected.Label() + " was not heard. A quiet survey knows only the devices this computer has talked to recently."
		case found == nil:
			st.selectNote = st.selected.Label() + " did not answer this time. That is not proof it has gone: silence is unknown, not empty."
		case by == "ip":
			st.selectNote = "Matched by address only, a weak match: on a network that hands out addresses, this may now be another device."
		}
		if found != nil && !isAlive(found) {
			found = nil
		}
		st.selected = found
	}
	if t.mapView != nil {
		t.mapView.SetGraph(g)
		t.mapView.Select(st.selected)
	}
}

// start runs a survey in the background. The map stays on the previous survey,
// dimmed, until the new one is in.
func (t *Tool) start(quiet bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.mu.Lock()
	t.cancel = cancel
	t.mu.Unlock()
	t.st.run = runState{running: true, quiet: quiet, stage: "orient"}
	t.rebuildLeft()

	t.running.Add(1)
	go func() {
		defer t.running.Done()
		defer cancel()
		g, prof, err := t.runner.Run(ctx, quiet, func(stage string, done, total int) {
			if t.closed.Load() {
				return
			}
			fyne.Do(func() {
				if !t.st.run.running {
					return
				}
				t.st.run.stage, t.st.run.done, t.st.run.total = stage, done, total
				if stage == "sweep" {
					t.st.run.sweepDone, t.st.run.sweepTotal = done, total
				}
				t.rebuildLeft()
			})
		})
		// A survey cut short is saved too, as `carta survey` saves what it
		// found by its deadline: partial and marked so, not thrown away.
		var saved string
		var saveErr error
		if err == nil && g != nil && t.dir != "" {
			var res pack.Result
			res, saveErr = pack.Save(t.dir, g, prof)
			saved = res.Dir
		}
		if !t.closed.Load() {
			fyne.Do(func() { t.finish(g, err, quiet, saved, saveErr) })
		}
	}()
}

// stop is the Stop button: it cancels the survey and keeps the window.
func (t *Tool) stop() {
	t.st.run.stopping = true
	t.rebuildLeft()
	t.mu.Lock()
	if t.cancel != nil {
		t.cancel()
	}
	t.mu.Unlock()
}

func (t *Tool) finish(g *model.Graph, err error, quiet bool, saved string, saveErr error) {
	t.mu.Lock()
	t.cancel = nil
	t.mu.Unlock()
	t.st.run = runState{}
	if err != nil || g == nil {
		if err != nil {
			t.st.run.err = err.Error()
		}
		t.rebuild()
		t.signal()
		return
	}
	t.st.run.stoppedIn = g.Meta["cut_short"]
	t.st.run.savedTo = saved
	if saveErr != nil {
		t.st.run.err = "it ran, but could not be saved: " + saveErr.Error()
	}
	t.show(g, &quiet)
	if t.st.selectNote != "" {
		t.st.tab = tabDevice
	}
	t.rebuild()
	t.signal()
}

func (t *Tool) signal() {
	if t.finished != nil {
		t.finished <- struct{}{}
	}
}

// ---------- Open ----------

func (t *Tool) openDialog() {
	if t.st.run.running {
		return
	}
	d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) {
		if err != nil || r == nil {
			return
		}
		path := r.URI().Path()
		r.Close()
		if err := t.open(path); err != nil {
			dialog.ShowError(err, t.w)
		}
	}, t.w)
	d.SetFilter(storage.NewExtensionFileFilter([]string{".json"}))
	d.SetView(dialog.ListView)
	if t.dir != "" {
		if l, err := storage.ListerForURI(storage.NewFileURI(t.dir)); err == nil {
			d.SetLocation(l)
		}
	}
	d.Resize(fyne.NewSize(760, 520))
	d.Show()
}

// open shows a saved survey. It is a survey to read, not a new one, so it is
// not compared with whatever was on screen before.
func (t *Tool) open(path string) error {
	g, err := snapshot.Read(path)
	if err != nil {
		return err
	}
	t.show(g, nil)
	t.st.earlier, t.st.drift = nil, nil
	t.st.run = runState{stoppedIn: g.Meta["cut_short"]}
	if t.st.selected == nil {
		t.selectGateway()
	}
	t.st.tab = tabDevice
	t.rebuild()
	return nil
}

// ---------- Compare ----------

// earlierSurveys lists the surveys saved in dir, newest first, without the one
// on screen.
func (t *Tool) earlierSurveys() []*model.Graph {
	if t.dir == "" {
		return t.demoEarlier
	}
	entries, _ := os.ReadDir(t.dir)
	var out []*model.Graph
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		g, err := snapshot.Read(filepath.Join(t.dir, e.Name(), "survey.json"))
		if err != nil || (t.st.g != nil && g.SurveyID == t.st.g.SurveyID) {
			continue
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

func (t *Tool) compareDialog() {
	if t.st.g == nil || t.st.run.running {
		return
	}
	var d dialog.Dialog
	content := t.comparePicker(t.earlierSurveys(), func(g *model.Graph) {
		d.Hide()
		if g == nil {
			t.clearComparison()
		} else {
			t.compareWith(g)
		}
	})
	d = dialog.NewCustom("Compare with an earlier survey", "Close", content, t.w)
	d.Resize(fyne.NewSize(560, 520))
	d.Show()
}

// compareWith shows what changed between an earlier survey and the one on
// screen, and opens the Drift tab.
func (t *Tool) compareWith(earlier *model.Graph) {
	r := drift.Compare(earlier, t.st.g)
	t.st.earlier, t.st.drift, t.st.tab = earlier, &r, tabDrift
	t.rebuildPanel()
}

func (t *Tool) clearComparison() {
	t.st.earlier, t.st.drift = nil, nil
	if t.st.tab == tabDrift {
		t.st.tab = tabDevice
	}
	t.rebuildPanel()
}
