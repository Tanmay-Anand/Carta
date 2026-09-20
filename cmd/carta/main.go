// Command carta surveys a network you are authorised to survey and writes the
// documentation nobody ever gets around to writing.
//
// Usage:
//
//	carta survey [--out DIR] [--passive] [--timeout 700ms] [--oui oui.csv]
//	carta diff BASELINE.json [CURRENT.json]
//	carta ui [SNAPSHOT.json]
//
// Everything it emits is a claim with evidence attached. Nothing it could not
// reach is drawn as if it were empty.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Tanmay-Anand/carta/internal/drift"
	"github.com/Tanmay-Anand/carta/internal/model"
	"github.com/Tanmay-Anand/carta/internal/render"
	"github.com/Tanmay-Anand/carta/internal/store"
	"github.com/Tanmay-Anand/carta/internal/survey"
	"github.com/Tanmay-Anand/carta/internal/ui"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "survey":
		err = cmdSurvey(ctx, os.Args[2:])
	case "diff":
		err = cmdDiff(os.Args[2:])
	case "ui":
		err = cmdUI(ctx, os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("carta " + version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "carta: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "carta:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `carta — plug in, and the network documents itself.

  carta survey [flags]        survey the network this machine is on
  carta diff A.json [B.json]  report what changed between two surveys
  carta ui [SNAPSHOT.json]    open the survey in a browser
  carta version

survey flags:
  --out DIR        where to write the document pack (default ./carta-surveys)
  --passive        emit nothing: read caches and listen for announcements only
  --timeout DUR    per-connection budget (default 700ms)
  --concurrency N  parallel workers (default 128)
  --oui FILE       an IEEE oui.csv to use instead of the built-in table
  --open           open the survey in a browser when it finishes

Survey only networks you are authorised to survey.
`)
}

func cmdSurvey(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("survey", flag.ExitOnError)
	out := fs.String("out", "carta-surveys", "directory for the document pack")
	passive := fs.Bool("passive", false, "emit nothing; caches and multicast listening only")
	timeout := fs.Duration("timeout", 700*time.Millisecond, "per-connection budget")
	conc := fs.Int("concurrency", 128, "parallel workers")
	oui := fs.String("oui", "", "path to an IEEE oui.csv")
	deadline := fs.Duration("deadline", 4*time.Minute, "hard limit on the whole survey")
	open := fs.Bool("open", false, "open the result in a browser")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "carta %s\n", version)
	start := time.Now()

	// A survey is bounded work, and a tool someone runs while standing at a
	// patch panel has to be able to promise when it will stop. Whatever has
	// been discovered by the deadline is still written out; the coverage
	// section then reports how far it got, which is an honest partial survey
	// rather than a hang.
	ctx, cancelDeadline := context.WithTimeout(ctx, *deadline)
	defer cancelDeadline()

	var lastStage string
	g, prof, err := survey.Run(ctx, survey.Options{
		Passive:     *passive,
		Concurrency: *conc,
		Timeout:     *timeout,
		OUIPath:     *oui,
		Progress: func(stage string, done, total int) {
			if stage != lastStage {
				lastStage = stage
				fmt.Fprintf(os.Stderr, "\n  %-8s %s", stage, elapsed(start))
			}
			if total > 0 && (done%16 == 0 || done == total) {
				fmt.Fprintf(os.Stderr, "\r  %-8s %s  %d/%d", stage, elapsed(start), done, total)
			}
		},
	})
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return err
	}

	dir := filepath.Join(*out, g.SurveyID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := store.WriteSnapshot(filepath.Join(dir, "survey.json"), g); err != nil {
		return err
	}
	if err := store.WriteDB(filepath.Join(dir, "survey.db"), g); err != nil {
		return err
	}
	pack, err := render.Pack(dir, g, prof)
	if err != nil {
		return err
	}

	// A baseline is just the most recent survey, kept at a stable path so the
	// next run has something to diff against without the operator having to
	// remember a filename.
	baseline := filepath.Join(*out, "baseline.json")
	if prev, err := store.ReadSnapshot(baseline); err == nil {
		rep := drift.Compare(prev, g)
		if err := os.WriteFile(filepath.Join(dir, "drift.md"), []byte(render.Drift(rep)), 0o644); err != nil {
			return err
		}
		pack = append(pack, filepath.Join(dir, "drift.md"))
		fmt.Fprintln(os.Stderr, "\n"+rep.Summary())
	}
	if err := store.WriteSnapshot(baseline, g); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\nSurveyed %s from %s in %s.\n",
		g.Vantage.Prefix, g.Vantage.IP, time.Since(start).Round(time.Millisecond))
	printSummary(g)
	fmt.Fprintf(os.Stderr, "\nWrote %d files to %s\n", len(pack), dir)
	for _, p := range pack {
		fmt.Fprintf(os.Stderr, "  %s\n", filepath.Base(p))
	}

	if *open {
		return ui.Serve(ctx, g, 0, true)
	}
	return nil
}

// printSummary is the thing an operator reads while still standing at the jack.
// It is deliberately short and deliberately includes what was NOT found.
func printSummary(g *model.Graph) {
	live, named, unidentified := 0, 0, 0
	classes := map[string]int{}
	for _, e := range g.Entities {
		if v, _ := e.Best("alive"); v != "yes" {
			continue
		}
		live++
		host, _ := e.Best("hostname")
		class, classConf := e.Best("class")
		if host != "" {
			named++
		}
		if host == "" && (class == "" || classConf <= model.Low) {
			unidentified++
		}
		if class != "" {
			classes[class]++
		}
	}
	fmt.Fprintf(os.Stderr, "\n  %d devices  ·  %d named  ·  %d unidentified\n", live, named, unidentified)

	keys := make([]string, 0, len(classes))
	for k := range classes {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return classes[keys[i]] > classes[keys[j]] })
	for _, k := range keys {
		fmt.Fprintf(os.Stderr, "    %-28s %d\n", k, classes[k])
	}
	for _, f := range g.Findings {
		if f.Severity == "info" {
			continue
		}
		fmt.Fprintf(os.Stderr, "  [%s] %s\n", strings.ToUpper(f.Severity), f.Title)
	}
}

func cmdDiff(args []string) error {
	fs := flag.NewFlagSet("diff", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: carta diff BASELINE.json [CURRENT.json]")
	}
	before, err := store.ReadSnapshot(fs.Arg(0))
	if err != nil {
		return err
	}
	var after *model.Graph
	if fs.NArg() >= 2 {
		after, err = store.ReadSnapshot(fs.Arg(1))
	} else {
		after, err = store.ReadSnapshot(filepath.Join(filepath.Dir(fs.Arg(0)), "baseline.json"))
	}
	if err != nil {
		return err
	}
	fmt.Print(render.Drift(drift.Compare(before, after)))
	return nil
}

func cmdUI(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("ui", flag.ExitOnError)
	port := fs.Int("port", 7373, "loopback port; 0 picks a free one")
	noOpen := fs.Bool("no-open", false, "do not open a browser")
	if err := fs.Parse(flagsFirst(args)); err != nil {
		return err
	}
	path := fs.Arg(0)
	if path == "" {
		path = filepath.Join("carta-surveys", "baseline.json")
	}
	g, err := store.ReadSnapshot(path)
	if err != nil {
		return fmt.Errorf("%s: %w (run `carta survey` first)", path, err)
	}
	return ui.Serve(ctx, g, *port, !*noOpen)
}

// flagsFirst reorders arguments so flags can follow the file name.
//
// Go's flag package stops parsing at the first positional argument, so
// "carta ui survey.json --no-open" silently ignores the flag. Every other
// command-line tool on the machine accepts that order, and silently doing
// something different from what was typed is worse than being strict.
func flagsFirst(args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			// A flag that takes a separate value carries it along: "--port 80"
			// is two arguments, "--port=80" is one.
			if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") && takesValue(a) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		positional = append(positional, a)
	}
	return append(flags, positional...)
}

// takesValue reports whether a flag consumes the next argument. Booleans do not.
func takesValue(flag string) bool {
	switch strings.TrimLeft(flag, "-") {
	case "port", "out", "timeout", "concurrency", "oui", "deadline":
		return true
	}
	return false
}

func elapsed(start time.Time) string {
	return fmt.Sprintf("%5.1fs", time.Since(start).Seconds())
}

// jsonDump is kept for debugging a survey without the renderers in the way.
func jsonDump(v any) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}
