// Command carta-desktop is Carta as a window: no browser, no server, one
// portable executable. It surveys the network this computer is on and keeps
// its surveys in carta-surveys next to itself.
//
//	carta-desktop          survey this network
//	carta-desktop --demo   a fictional network and a survey that sends nothing
package main

import (
	_ "embed"
	"os"
	"path/filepath"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"

	"github.com/Tanmay-Anand/carta/desktop/kit"
	"github.com/Tanmay-Anand/carta/desktop/shell"
	"github.com/Tanmay-Anand/carta/desktop/tools/survey"
)

// icon is the window and taskbar icon. `fyne package` also puts it on the
// .exe itself; setting it here means a plain `go build` shows it too.
//
//go:embed Icon.png
var icon []byte

func main() {
	a := app.NewWithID("io.github.tanmayanand.carta.desktop")
	a.SetIcon(fyne.NewStaticResource("Icon.png", icon))
	a.Settings().SetTheme(shell.Theme{})

	w := a.NewWindow("Carta")
	var tool *survey.Tool
	if len(os.Args) > 1 && os.Args[1] == "--demo" {
		// A fictional network and a stand-in survey that sends nothing: for
		// screenshots and for trying the app, where no real network belongs.
		tool = survey.NewDemo()
	} else {
		tool = survey.New(surveyDir())
	}
	tools := []shell.Tool{tool}
	w.SetContent(shell.New(w, tools, func(name string, active bool, tapped func()) fyne.CanvasObject {
		b := kit.NewTextButton(name, 14, tapped)
		b.Active = active
		return container.NewCenter(b)
	}))
	w.SetCloseIntercept(func() {
		for _, t := range tools {
			t.Stop()
		}
		// The process must not outlive its window. On rare runs on this
		// machine (hybrid NVIDIA and AMD graphics) the process stayed alive,
		// windowless and idle, after the window closed; it was not
		// reproducible on demand, so the exit is made certain rather than
		// trusted: quit, and if that has not ended the process within three
		// seconds, exit. Tools have stopped by now, and nothing is written
		// after Stop returns.
		go func() {
			time.Sleep(3 * time.Second)
			os.Exit(0)
		}()
		w.Close()
		a.Quit()
	})
	w.Resize(fyne.NewSize(1240, 740))
	w.CenterOnScreen()
	w.ShowAndRun()
}

// surveyDir is where surveys are kept: carta-surveys next to the program, so
// the app and its surveys travel together on a USB stick. If that folder
// cannot be written (the program sits in Program Files, say), the user's own
// configuration folder is used instead.
func surveyDir() string {
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Join(filepath.Dir(exe), "carta-surveys")
		if writable(dir) {
			return dir
		}
	}
	if cfg, err := os.UserConfigDir(); err == nil {
		dir := filepath.Join(cfg, "Carta", "carta-surveys")
		if writable(dir) {
			return dir
		}
	}
	return "carta-surveys"
}

func writable(dir string) bool {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}
