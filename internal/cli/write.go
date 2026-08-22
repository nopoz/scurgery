package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nopoz/scurgery/internal/api"
)

// Env carries everything a command needs that is not its own arguments, so
// tests can supply a fake tailnet and capture output.
type Env struct {
	Client    *api.Client
	Out       io.Writer
	In        io.Reader
	BackupDir string
	AssumeYes bool
	DryRun    bool
}

// ErrDeclined reports that the operator answered no at the confirmation
// prompt. It is an error so that the process exits non-zero.
var ErrDeclined = errors.New("declined")

// writePolicy runs the safety ladder for every mutation:
//
//  1. read the policy and its ETag
//  2. refuse to proceed if the server returned no ETag, since the write
//     could not be conditioned safely
//  3. compute the new policy
//  4. self-check: assert scurgery changed only what it owns
//  5. ask the server to validate it, requiring an empty body
//  6. show a diff (a dry run stops here)
//  7. back up the pre-change policy locally
//  8. confirm
//  9. write conditionally on the ETag
//
// Steps 4 and 5 are independent checks on the same claim: 4 catches "scurgery
// mangled something", 5 catches "Tailscale will not accept this".
//
// mutate returns nil to mean there is nothing to do.
func writePolicy(
	ctx context.Context,
	env *Env,
	what string,
	mutate func(current []byte) ([]byte, error),
	verify func(before, after []byte) error,
) error {
	current, etag, err := env.Client.GetPolicy(ctx)
	if err != nil {
		return err
	}
	if etag == "" {
		return fmt.Errorf("%s: the tailnet did not return an ETag, so scurgery cannot write back safely with a compare-and-swap; refusing to write", what)
	}

	next, err := mutate(current)
	if err != nil {
		return err
	}
	if next == nil || string(next) == string(current) {
		fmt.Fprintf(env.Out, "%s: no change needed\n", what)
		return nil
	}

	if verify != nil {
		if err := verify(current, next); err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
	}

	if err := env.Client.Validate(ctx, next); err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}

	diff := Unified(current, next, 3)
	fmt.Fprintf(env.Out, "%s\n", diff)

	if env.DryRun {
		fmt.Fprintf(env.Out, "%s: dry run, nothing written\n", what)
		return nil
	}

	backup, err := writeBackup(env.BackupDir, current)
	if err != nil {
		return fmt.Errorf("writing backup: %w", err)
	}
	fmt.Fprintf(env.Out, "backed up current policy to %s\n", backup)

	if !env.AssumeYes {
		ok, err := confirm(env, fmt.Sprintf("apply this change to the tailnet policy? [y/N] "))
		if err != nil {
			return err
		}
		if !ok {
			return ErrDeclined
		}
	}

	if err := env.Client.SetPolicy(ctx, next, etag); err != nil {
		if errors.Is(err, api.ErrPreconditionFailed) {
			return fmt.Errorf("the policy changed on the tailnet while scurgery was working, so nothing was written. Re-run to pick up the current policy. (%w)", err)
		}
		return err
	}
	fmt.Fprintf(env.Out, "%s: done\n", what)
	return nil
}

func writeBackup(dir string, policy []byte) (string, error) {
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := filepath.Join(dir, fmt.Sprintf("policy-backup-%s.hujson", time.Now().UTC().Format("20060102-150405")))
	if err := os.WriteFile(name, policy, 0o600); err != nil {
		return "", err
	}
	return name, nil
}

func confirm(env *Env, prompt string) (bool, error) {
	fmt.Fprint(env.Out, prompt)
	r := bufio.NewReader(env.In)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
