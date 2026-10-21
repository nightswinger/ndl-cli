// Package cli wires the cobra commands for the ndl tool.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/nightswinger/ndl/internal/client"
	"github.com/nightswinger/ndl/internal/oaipmh"
	"github.com/nightswinger/ndl/internal/opensearch"
	"github.com/nightswinger/ndl/internal/output"
	"github.com/nightswinger/ndl/internal/sru"
)

// Version is overridden at build time with -ldflags "-X .../cli.Version=...".
var Version = "dev"

// retryBackoff is the first retry delay (overridden in tests).
var retryBackoff = time.Second

// Exit codes.
const (
	ExitOK          = 0
	ExitAPI         = 1
	ExitUsage       = 2
	ExitNetwork     = 3
	ExitInterrupted = 130
)

// usageError marks argument errors (exit code 2).
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

func usagef(format string, args ...any) error {
	return usageError{fmt.Errorf(format, args...)}
}

type app struct {
	stdout, stderr io.Writer

	format   string
	dryRun   bool
	baseURL  string
	timeout  time.Duration
	interval time.Duration
	verbose  bool
	quiet    bool
	fields   []string

	// normalized at PersistentPreRun
	outFields []string
}

// Execute runs the CLI and returns the process exit code.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	a := &app{stdout: stdout, stderr: stderr}
	root := a.rootCmd()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	err := root.ExecuteContext(ctx)
	if err == nil {
		return ExitOK
	}
	code := exitCode(err)
	if code != ExitInterrupted {
		fmt.Fprintln(stderr, "ndl: error:", err)
	}
	if code == ExitUsage && !errors.As(err, new(usageError)) {
		fmt.Fprintln(stderr, "Run 'ndl --help' for usage.")
	}
	return code
}

func exitCode(err error) int {
	var ue usageError
	var ne *client.NetworkError
	switch {
	case errors.Is(err, context.Canceled):
		return ExitInterrupted
	case errors.As(err, &ue):
		return ExitUsage
	case errors.As(err, &ne), errors.Is(err, context.DeadlineExceeded):
		return ExitNetwork
	}
	var de *sru.DiagnosticsError
	var oe oaipmh.ErrorList
	var ae *opensearch.APIError
	var he *client.HTTPError
	if errors.As(err, &de) || errors.As(err, &oe) || errors.As(err, &ae) || errors.As(err, &he) {
		return ExitAPI
	}
	// Remaining errors come from cobra's own argument/flag parsing.
	msg := err.Error()
	for _, p := range []string{"unknown command", "unknown flag", "unknown shorthand", "invalid argument", "flag needs an argument", "accepts ", "requires ", "required flag"} {
		if strings.Contains(msg, p) {
			return ExitUsage
		}
	}
	return ExitAPI
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// parseDuration accepts Go durations ("30s") or plain seconds ("30").
func parseDuration(s string) (time.Duration, error) {
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Duration(n * float64(time.Second)), nil
	}
	return time.ParseDuration(s)
}

func (a *app) rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "ndl",
		Short: "Command-line client for the National Diet Library Search (NDLサーチ) APIs",
		Long: `ndl wraps the NDL Search external APIs: SRU, OpenSearch and OAI-PMH.

本ツールは国立国会図書館サーチ（NDLサーチ）の外部提供APIを利用しています。
利用条件: https://ndlsearch.ndl.go.jp/help/api

Exit codes: 0 success, 1 API diagnostic/error, 2 usage error, 3 network error.`,
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if !output.ValidFormat(a.format) {
				return usagef("--format must be one of %s", strings.Join(output.Formats, ", "))
			}
			if a.interval < 0 || a.timeout <= 0 {
				return usagef("--interval must be >= 0 and --timeout > 0")
			}
			f, err := output.NormalizeFields(a.fields)
			if err != nil {
				return usageError{err}
			}
			a.outFields = f
			return nil
		},
	}
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error { return usageError{err} })

	timeoutDef := client.DefaultTimeout
	if s := os.Getenv("NDL_TIMEOUT"); s != "" {
		if d, err := parseDuration(s); err == nil {
			timeoutDef = d
		}
	}

	pf := root.PersistentFlags()
	pf.StringVar(&a.format, "format", "table", "output format: table|json|jsonl|xml (xml prints the raw API response)")
	pf.BoolVar(&a.dryRun, "dry-run", false, "print the request URL without sending it")
	pf.StringVar(&a.baseURL, "base-url", envDefault("NDL_BASE_URL", client.DefaultBaseURL), "API base URL [$NDL_BASE_URL]")
	pf.DurationVar(&a.timeout, "timeout", timeoutDef, "per-request timeout [$NDL_TIMEOUT]")
	pf.DurationVar(&a.interval, "interval", client.DefaultInterval, "minimum wait between consecutive requests")
	pf.BoolVarP(&a.verbose, "verbose", "v", false, "log requests to stderr")
	pf.BoolVarP(&a.quiet, "quiet", "q", false, "suppress result counts and progress on stderr")
	pf.StringSliceVar(&a.fields, "fields", nil, "comma-separated fields for table/json/jsonl (e.g. title,creator,date,identifier,url)")

	root.AddCommand(a.sruCmd(), a.openSearchCmd(), a.oaipmhCmd())
	return root
}

func (a *app) client() *client.Client {
	cfg := client.Config{
		BaseURL:    a.baseURL,
		Timeout:    a.timeout,
		Interval:   a.interval,
		UserAgent:  client.UserAgent(Version),
		MaxRetries: client.DefaultMaxRetries,
		Backoff:    retryBackoff,
	}
	if a.verbose {
		cfg.Log = a.stderr
	}
	return client.New(cfg)
}

// info writes a status line to stderr unless --quiet.
func (a *app) info(format string, args ...any) {
	if !a.quiet {
		fmt.Fprintf(a.stderr, format+"\n", args...)
	}
}

// warn always writes to stderr.
func (a *app) warn(format string, args ...any) {
	fmt.Fprintf(a.stderr, "ndl: warning: "+format+"\n", args...)
}

func (a *app) outputOptions() output.Options {
	return output.Options{Format: a.format, Fields: a.outFields}
}

// writeRaw prints a raw XML response for --format xml.
func (a *app) writeRaw(raw []byte) error {
	if _, err := a.stdout.Write(raw); err != nil {
		return err
	}
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		_, err := io.WriteString(a.stdout, "\n")
		return err
	}
	return nil
}

// noArgsUsage rejects positional arguments with a usage error.
func noArgsUsage(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return usagef("%s takes no arguments, got %q", cmd.CommandPath(), args)
	}
	return nil
}

// renameFlags maps alternative spellings onto canonical flag names.
func renameFlags(aliases map[string]string) func(*pflag.FlagSet, string) pflag.NormalizedName {
	return func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		if to, ok := aliases[name]; ok {
			return pflag.NormalizedName(to)
		}
		return pflag.NormalizedName(name)
	}
}
