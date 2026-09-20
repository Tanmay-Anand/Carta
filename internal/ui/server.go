// Package ui serves the survey to a browser on loopback.
//
// A browser is the one rich display guaranteed to exist on a borrowed machine.
// A native window would need a toolkit; Electron would need 300MB; a platform
// WebView is not guaranteed to be installed. An HTTP server bound to 127.0.0.1
// needs nothing at all, and closing the tab costs nothing.
//
// The assets are embedded in the binary and nothing is fetched at runtime — no
// CDN, no fonts, no map tiles. The tool is meant to run on air-gapped and
// segregated networks, and a UI that silently needs the internet is a UI that
// fails exactly where this tool is most useful.
package ui

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/Tanmay-Anand/carta/internal/model"
)

//go:embed assets/*
var assets embed.FS

// Serve starts the loopback server and blocks until the context is cancelled.
func Serve(ctx context.Context, g *model.Graph, port int, open bool) error {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/api/survey", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(g)
	})

	// Binding to loopback specifically, not to :port. A survey document lists
	// every device on a network and where its management interface is; serving
	// that on 0.0.0.0 would publish the map of the network to the network.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil && port != 0 {
		// Something else already holds the port. On a borrowed machine that is
		// ordinary and none of the operator's business, so take any free port
		// and print where it landed rather than failing with a bind error.
		fmt.Fprintf(os.Stderr, "carta: port %d is in use, picking a free one\n", port)
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		return fmt.Errorf("ui: %w", err)
	}
	url := fmt.Sprintf("http://%s/", ln.Addr().String())
	fmt.Fprintf(os.Stderr, "\ncarta ui — %s  (ctrl-c to stop)\n", url)

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	if open {
		openBrowser(url)
	}
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// openBrowser is best-effort by design: on a hardened machine the browser may
// not launch, and the URL printed above is the fallback that always works.
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
