package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
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
  scurgery status                  list installed bundles
  scurgery diff   <bundle.hujson>  show what apply would change, write nothing

Credentials come from the environment:
  TS_API_KEY   a Tailscale API access token
  TS_TAILNET   the tailnet name, as shown in the admin console
`

func Run(ctx context.Context, args []string, stdout, stderr io.Writer, stdin io.Reader) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	cmd, rest := args[0], args[1:]

	fs := flag.NewFlagSet("scurgery "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		name            = fs.String("name", "", "bundle namespace (defaults to the filename stem)")
		dryRun          = fs.Bool("dry-run", false, "show the change, write nothing")
		yes             = fs.Bool("yes", false, "do not prompt for confirmation")
		force           = fs.Bool("force", false, "overwrite conflicting members")
		skipConflicts   = fs.Bool("skip-conflicts", false, "install everything except conflicts")
		matchStructural = fs.Bool("match-structural", false, "remove by content when markers are absent")
		tailnet         = fs.String("tailnet", os.Getenv("TS_TAILNET"), "tailnet name")
		backupDir       = fs.String("backup-dir", ".", "directory for pre-change policy backups")
	)
	flagArgs, positional := splitArgs(rest)
	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}

	if cmd == "help" || cmd == "-h" || cmd == "--help" {
		fmt.Fprint(stdout, usage)
		return 0
	}

	token := os.Getenv("TS_API_KEY")
	if token == "" {
		fmt.Fprintln(stderr, "error: TS_API_KEY is not set. Create an API access token under Settings, Keys in the admin console")
		return 1
	}
	if *tailnet == "" {
		fmt.Fprintln(stderr, "error: no tailnet. Set TS_TAILNET or pass --tailnet")
		return 1
	}

	client := api.New(token, *tailnet)
	if base := os.Getenv("TS_BASE_URL"); base != "" {
		client.BaseURL = base
	}

	env := &Env{
		Client:    client,
		Out:       stdout,
		In:        stdin,
		BackupDir: *backupDir,
		AssumeYes: *yes,
		DryRun:    *dryRun,
	}

	var err error
	switch cmd {
	case "apply":
		if len(positional) < 1 {
			fmt.Fprintln(stderr, "error: apply needs a bundle file")
			return 2
		}
		err = runApply(ctx, env, positional[0], *name, policy.ApplyOptions{Force: *force, SkipConflicts: *skipConflicts})
	case "remove":
		if len(positional) < 1 {
			fmt.Fprintln(stderr, "error: remove needs a bundle name or file")
			return 2
		}
		err = runRemove(ctx, env, positional[0], *name, *matchStructural)
	case "status":
		err = runStatus(ctx, env)
	case "diff":
		if len(positional) < 1 {
			fmt.Fprintln(stderr, "error: diff needs a bundle file")
			return 2
		}
		env.DryRun = true
		env.AssumeYes = true
		err = runApply(ctx, env, positional[0], *name, policy.ApplyOptions{Force: *force, SkipConflicts: *skipConflicts})
	default:
		fmt.Fprintf(stderr, "error: unknown command %q\n\n%s", cmd, usage)
		return 2
	}

	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
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
	if len(names) == 0 {
		fmt.Fprintln(env.Out, "nothing installed by scurgery")
		return nil
	}
	fmt.Fprintf(env.Out, "installed: %s\n", strings.Join(names, ", "))
	return nil
}
