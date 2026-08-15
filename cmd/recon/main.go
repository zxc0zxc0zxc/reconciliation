// Command recon runs one rule from a config file and prints the result.
// It exits 0 on a clean run, 1 when findings exist, and 2 on failure.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/zxc0zxc0zxc/reconciliation/config"
	"github.com/zxc0zxc0zxc/reconciliation/recon"
	"github.com/zxc0zxc0zxc/reconciliation/report"
	"github.com/zxc0zxc0zxc/reconciliation/runner"
	"github.com/zxc0zxc0zxc/reconciliation/source"
)

const (
	exitClean    = 0
	exitFindings = 1
	exitError    = 2
)

func main() {
	os.Exit(run())
}

func run() int {
	var (
		configPath = flag.String("config", "reconciliation.yaml", "path to the config file")
		ruleName   = flag.String("rule", "", "rule to run")
		list       = flag.Bool("list", false, "list configured rules and exit")
		from       = flag.String("from", "", "window start, RFC3339; defaults to -to minus -since")
		to         = flag.String("to", "", "window end, RFC3339; defaults to now")
		since      = flag.Duration("since", 24*time.Hour, "window length when -from is omitted")
		format     = flag.String("format", "text", "output format: text or json")
	)
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fail(err)
	}
	if *list {
		fmt.Println(strings.Join(cfg.RuleNames(), "\n"))
		return exitClean
	}
	if *ruleName == "" {
		return fail(errors.New("no rule given: pass -rule, or -list to see the configured ones"))
	}

	window, err := parseWindow(*from, *to, *since)
	if err != nil {
		return fail(err)
	}
	rule, endpoints, err := cfg.Lookup(*ruleName)
	if err != nil {
		return fail(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	clients := make([]*source.Client, 0, len(endpoints))
	defer func() {
		for _, c := range clients {
			if err := c.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "close %s: %v\n", c.Name(), err)
			}
		}
	}()
	for _, endpoint := range endpoints {
		client, err := source.Dial(endpoint)
		if err != nil {
			return fail(err)
		}
		clients = append(clients, client)
	}

	res, err := runner.Run(ctx, rule, clients[0], clients[1], window)
	if err != nil {
		return fail(err)
	}

	if err := write(*format, res); err != nil {
		return fail(err)
	}
	if !res.Clean() {
		return exitFindings
	}
	return exitClean
}

func write(format string, res recon.Result) error {
	switch format {
	case "text":
		return report.Text(os.Stdout, res)
	case "json":
		return report.JSON(os.Stdout, res)
	default:
		return fmt.Errorf("unknown format %q: want text or json", format)
	}
}

func parseWindow(from, to string, since time.Duration) (recon.Window, error) {
	end := time.Now().UTC()
	if to != "" {
		parsed, err := time.Parse(time.RFC3339, to)
		if err != nil {
			return recon.Window{}, fmt.Errorf("parse -to: %w", err)
		}
		end = parsed
	}
	start := end.Add(-since)
	if from != "" {
		parsed, err := time.Parse(time.RFC3339, from)
		if err != nil {
			return recon.Window{}, fmt.Errorf("parse -from: %w", err)
		}
		start = parsed
	}
	if !start.Before(end) {
		return recon.Window{}, errors.New("window start must precede its end")
	}
	return recon.Window{From: start, To: end}, nil
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "recon:", err)
	return exitError
}
