package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/nopoz/scurgery/internal/api"
	"github.com/nopoz/scurgery/internal/policy"
)

const usage = `scurgery adds a named set of blocks to a tailnet policy file and removes
exactly those blocks again, leaving the rest of the file untouched.

  scurgery apply  <bundle.hujson>  [--name N] [--dry-run] [--yes]
                                   [--force] [--skip-conflicts]
  scurgery remove <name>           [--dry-run] [--yes]
  scurgery remove <bundle.hujson>  [--name N] [--dry-run] [--yes]
                                   [--match-structural]
  scurgery status                  list installed bundles [--json]
  scurgery diff   <bundle.hujson>  show what apply would change, write nothing
                                   [--json]

Exit codes:
  0  success; for diff, nothing would change
  1  diff only: applying the bundle would change the policy
  2  usage error
  3  runtime error: credentials, network, or a refused write

Credentials come from the environment:
  TS_API_KEY   a Tailscale API access token
  TS_TAILNET   the tailnet name, as shown in the admin console

Those two names may also live in a config file, one KEY=VALUE per line, at
~/.config/scurgery/config (or under $XDG_CONFIG_HOME). It must not be
readable by anyone but its owner. The environment and --tailnet win over it.
`

// Exit codes are a contract automation depends on: a caller must be able to
// tell "the policy would change" apart from "scurgery could not tell you".
const (
	exitOK      = 0 // success, and for diff, nothing would change
	exitChanged = 1 // diff only: applying this bundle would change the policy
	exitUsage   = 2 // the command line was wrong
	exitError   = 3 // everything else that failed: credentials, network, refusal
)

// newClient is api.New behind a package variable so tests can point Run at a
// fake server. Unlike an environment variable it is not settable at runtime,
// so it cannot become a way for anyone but the test binary itself to
// redirect where credentials and policy bodies are sent.
var newClient = api.New

func Run(ctx context.Context, args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}

	cmd, rest := args[0], args[1:]

	if cmd == "help" || cmd == "-h" || cmd == "--help" {
		fmt.Fprint(stdout, usage)
		return 0
	}

	switch cmd {
	case "apply", "remove", "status", "diff":
	default:
		fmt.Fprintf(stderr, "error: unknown command %q\n\n%s", cmd, usage)
		return exitUsage
	}

	cfgPath := configPath()
	cfg, cfgErr := loadConfig(cfgPath)
	if cfgErr != nil {
		fmt.Fprintf(stderr, "error: %v\n", cfgErr)
		return exitError
	}

	fs := flag.NewFlagSet("scurgery "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		name            = fs.String("name", "", "bundle namespace (defaults to the filename stem)")
		dryRun          = fs.Bool("dry-run", false, "show the change, write nothing")
		yes             = fs.Bool("yes", false, "do not prompt for confirmation")
		force           = fs.Bool("force", false, "overwrite conflicting members")
		skipConflicts   = fs.Bool("skip-conflicts", false, "install everything except conflicts")
		matchStructural = fs.Bool("match-structural", false, "remove by content when markers are absent")
		tailnet         = fs.String("tailnet", credential("TS_TAILNET", cfg), "tailnet name")
		backupDir       = fs.String("backup-dir", ".", "directory for pre-change policy backups")
		jsonOut         = fs.Bool("json", false, "machine-readable output (status and diff only)")
	)
	flagArgs, positional := splitArgs(rest)
	if err := fs.Parse(flagArgs); err != nil {
		return exitUsage
	}
	if len(positional) > 1 {
		fmt.Fprintf(stderr, "error: too many arguments: %s\n", strings.Join(positional, ", "))
		return exitUsage
	}

	var bundleOrName string
	switch cmd {
	case "apply", "diff":
		if len(positional) < 1 {
			fmt.Fprintf(stderr, "error: %s needs a bundle file\n", cmd)
			return exitUsage
		}
		bundleOrName = positional[0]
	case "remove":
		if len(positional) < 1 {
			fmt.Fprintln(stderr, "error: remove needs a bundle name or file")
			return exitUsage
		}
		bundleOrName = positional[0]
	}

	if *jsonOut && cmd != "status" && cmd != "diff" {
		fmt.Fprintf(stderr, "error: --json is supported by status and diff only, not %s\n", cmd)
		return exitUsage
	}

	token := credential("TS_API_KEY", cfg)
	if token == "" {
		fmt.Fprintf(stderr, "error: no API token. Set TS_API_KEY%s. Create an access token under Settings, Keys in the admin console\n", configHint(cfgPath))
		return exitError
	}
	if *tailnet == "" {
		fmt.Fprintf(stderr, "error: no tailnet. Set TS_TAILNET, pass --tailnet%s\n", configHint(cfgPath))
		return exitError
	}

	env := &Env{
		Client:    newClient(token, *tailnet),
		Out:       stdout,
		In:        stdin,
		BackupDir: *backupDir,
		AssumeYes: *yes,
		DryRun:    *dryRun,
		JSON:      *jsonOut,
	}

	var err error
	switch cmd {
	case "apply":
		err = runApply(ctx, env, bundleOrName, *name, policy.ApplyOptions{Force: *force, SkipConflicts: *skipConflicts})
	case "remove":
		err = runRemove(ctx, env, bundleOrName, *name, *matchStructural)
	case "status":
		err = runStatus(ctx, env)
	case "diff":
		env.DryRun = true
		env.AssumeYes = true
		if env.JSON {
			// Prose moves to stderr rather than being dropped: conflict
			// detail is the most useful thing diff prints.
			env.Out = stderr
		}
		err = runApply(ctx, env, bundleOrName, *name, policy.ApplyOptions{Force: *force, SkipConflicts: *skipConflicts})
	}

	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitError
	}
	if cmd == "diff" && env.JSON {
		if err := writeJSON(stdout, diffReport{Bundle: env.Bundle, Changed: env.Changed, Diff: env.Diff}); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return exitError
		}
	}
	if cmd == "diff" && env.Changed {
		return exitChanged
	}
	return exitOK
}

// diffReport and statusReport are the machine-readable contract, so their
// field names change only deliberately.
type diffReport struct {
	Bundle  string `json:"bundle"`
	Changed bool   `json:"changed"`
	Diff    string `json:"diff"`
}

type statusReport struct {
	Installed []string `json:"installed"`
}

func writeJSON(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}

// splitArgs separates flags from positional arguments. Go's flag package stops
// parsing at the first non-flag argument, so without this a trailing
// "--yes" after the bundle path would be silently ignored rather than obeyed,
// which on a confirmation flag is a bad way to be wrong.
func splitArgs(args []string) (flags, positional []string) {
	boolFlags := map[string]bool{
		"dry-run":          true,
		"yes":              true,
		"force":            true,
		"skip-conflicts":   true,
		"match-structural": true,
		"json":             true,
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			return flags, positional
		}
		if len(a) > 1 && strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if strings.Contains(name, "=") {
				continue // value given inline
			}
			if !boolFlags[name] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		positional = append(positional, a)
	}
	return flags, positional
}

func runStatus(ctx context.Context, env *Env) error {
	current, _, err := env.Client.GetPolicy(ctx)
	if err != nil {
		return err
	}
	names, err := policy.Namespaces(current)
	if err != nil {
		return err
	}
	if env.JSON {
		return writeJSON(env.Out, statusReport{Installed: names})
	}
	if len(names) == 0 {
		fmt.Fprintln(env.Out, "nothing installed by scurgery")
		return nil
	}
	fmt.Fprintf(env.Out, "installed: %s\n", strings.Join(names, ", "))
	return nil
}
