package survey

import (
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/Tanmay-Anand/carta/desktop/shell"
)

// opened builds the demo tool in a headless window, with a stand-in survey
// that sends nothing and takes step per stage.
func opened(t *testing.T, step time.Duration) *Tool {
	t.Helper()
	tool := NewDemo()
	tool.runner = fakeRunner{step: step}
	return inWindow(t, tool)
}

// openedIn builds the real tool on a survey folder, still with the stand-in.
func openedIn(t *testing.T, dir string) *Tool {
	t.Helper()
	return inWindow(t, newTool(dir, fakeRunner{step: time.Millisecond}))
}

func inWindow(t *testing.T, tool *Tool) *Tool {
	t.Helper()
	test.NewTempApp(t).Settings().SetTheme(shell.Theme{}) // Carta's text sizes, as the app has
	tool.finished = make(chan struct{}, 1)
	w := test.NewTempWindow(t, nil)
	w.SetContent(tool.View(w))
	w.Resize(fyne.NewSize(1240, 740))
	return tool
}

func byLabel(t *testing.T, tool *Tool, label string) *Tool {
	t.Helper()
	for _, e := range alive(tool.st.g) {
		if e.Label() == label {
			tool.selectEntity(e)
			return tool
		}
	}
	t.Fatalf("no device %q", label)
	return nil
}

// texts lists every piece of text under an object, so a test can ask what a
// panel says without knowing how it is built.
func texts(o fyne.CanvasObject) []string {
	var out []string
	for _, c := range test.LaidOutObjects(o) {
		switch v := c.(type) {
		case *canvas.Text:
			out = append(out, v.Text)
		case *widget.Label:
			out = append(out, v.Text)
		}
	}
	return out
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func waitFinished(t *testing.T, tool *Tool) {
	t.Helper()
	select {
	case <-tool.finished:
	case <-time.After(10 * time.Second):
		t.Fatal("the survey did not finish")
	}
}

func TestSelectingADeviceFillsTheDevicePanel(t *testing.T) {
	tool := byLabel(t, opened(t, time.Millisecond), "nas.local")
	if tool.st.tab != tabDevice || tool.mapView.selected == nil || tool.mapView.selected.label != "nas.local" {
		t.Fatalf("selection did not reach the map and the panel: tab %d", tool.st.tab)
	}
	got := texts(tool.right)
	for _, want := range []string{"nas.local", "storage / NAS", "asserted", "Contoso Storage"} {
		if !contains(got, want) {
			t.Errorf("the panel does not show %q: %v", want, got)
		}
	}
	// Name and Class always show; one with no value says so.
	byLabel(t, tool, "192.168.10.77")
	if !contains(texts(tool.right), "not learned") {
		t.Error("an unnamed device does not say its name was not learned")
	}
}

func TestAFindingOpensTheDeviceItIsAbout(t *testing.T) {
	tool := opened(t, time.Millisecond)
	tool.st.tab = tabFindings
	tool.rebuildPanel()
	var rows []*clickable
	for _, o := range test.LaidOutObjects(tool.right) {
		if c, ok := o.(*clickable); ok {
			rows = append(rows, c)
		}
	}
	if len(rows) == 0 {
		t.Fatal("no finding rows")
	}
	test.Tap(rows[0]) // the camera
	if tool.st.selected == nil || tool.st.selected.Label() != "192.168.10.60" || tool.st.tab != tabDevice {
		t.Errorf("the risk finding opened %v on tab %d", tool.st.selected, tool.st.tab)
	}
}

func TestTheFilterNarrowsTheDeviceList(t *testing.T) {
	tool := opened(t, time.Millisecond)
	tool.st.tab = tabDevices
	tool.rebuildPanel()
	var entry *widget.Entry
	for _, o := range test.LaidOutObjects(tool.right) {
		if e, ok := o.(*widget.Entry); ok {
			entry = e
		}
	}
	if entry == nil {
		t.Fatal("no filter field")
	}
	test.Type(entry, "print")
	got := texts(tool.right)
	if !contains(got, "office-printer.local") || contains(got, "nas.local") {
		t.Errorf("filtering by %q left %v", "print", got)
	}
}

func TestASurveyReplacesTheMapAndKeepsTheSelection(t *testing.T) {
	tool := opened(t, time.Millisecond)
	before := tool.st.g
	byLabel(t, tool, "nas.local")
	tool.start(false)
	waitFinished(t, tool)
	if tool.st.g == before || tool.st.earlier != before || tool.st.drift == nil {
		t.Fatal("the new survey did not replace the old one, or no drift was computed")
	}
	if tool.st.selected == nil || tool.st.selected.Label() != "nas.local" || tool.st.selectNote != "" {
		t.Errorf("nas.local should stay selected by its MAC, quietly: %v %q", tool.st.selected, tool.st.selectNote)
	}
	if tool.st.run.running || tool.st.run.stoppedIn != "" {
		t.Errorf("a complete survey left run state %+v", tool.st.run)
	}
}

func TestStopKeepsAPartialSurveyAndSaysSo(t *testing.T) {
	tool := opened(t, 20*time.Millisecond)
	byLabel(t, tool, "192.168.10.77")
	tool.start(false)
	deadline := time.Now().Add(10 * time.Second)
	for {
		var swept int
		fyne.DoAndWait(func() { swept = tool.st.run.sweepDone })
		if swept >= 60 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the sweep never got going")
		}
		time.Sleep(10 * time.Millisecond)
	}
	fyne.DoAndWait(tool.stop)
	waitFinished(t, tool)
	if tool.st.run.stoppedIn != "sweep" {
		t.Fatalf("stopped in %q, want sweep", tool.st.run.stoppedIn)
	}
	_, _, _, unreached := coverage(tool.st.g)
	if unreached == 0 {
		t.Error("a stopped sweep shows no unreached addresses")
	}
	// .77 lies beyond the point the sweep reached, so it was never looked for.
	if tool.st.selected != nil || !strings.Contains(tool.st.selectNote, "stopped during sweeping") {
		t.Errorf("selection %v, note %q", tool.st.selected, tool.st.selectNote)
	}
	got := texts(tool.left)
	if !contains(got, "Stopped during Sweeping") || !contains(got, "STOPPED EARLY") {
		t.Errorf("the summary does not say the survey stopped early: %v", got)
	}
}

func TestAQuietSurveyKeepsOnlyWhatThisMachineKnew(t *testing.T) {
	tool := opened(t, time.Millisecond)
	tool.start(true)
	waitFinished(t, tool)
	if n := len(alive(tool.st.g)); n != 4 {
		t.Errorf("a quiet survey of the fictional network found %d devices, want the 4 in the ARP cache", n)
	}
	if !quietSurvey(tool.st.g) {
		t.Error("the result is not marked as a quiet survey")
	}
	if !contains(texts(tool.left), "Quiet survey: nothing was sent") {
		t.Error("the summary does not say nothing was sent")
	}
}
