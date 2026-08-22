package cli

import (
	"context"
	"fmt"

	"github.com/nopoz/scurgery/internal/bundle"
	"github.com/nopoz/scurgery/internal/policy"
)

// applyVerify is policy.VerifyApply behind a package variable so tests can
// wrap it to observe whether the self-check actually ran, rather than
// inferring that from output or side effects.
var applyVerify = policy.VerifyApply

func runApply(ctx context.Context, env *Env, bundlePath, nameOverride string, opts policy.ApplyOptions) error {
	b, err := bundle.Load(bundlePath, nameOverride)
	if err != nil {
		return err
	}

	// A --force overwrite replaces an operator's value in place without a
	// scurgery marker (see policy.Apply), so the self-check cannot tell that
	// apart from collateral damage: both look like "content changed that
	// scurgery does not own". When force actually overwrites something,
	// skip the self-check for this apply rather than have it reject an
	// overwrite the operator asked for. --skip-conflicts alone overwrites
	// nothing, so it must always keep the full self-check.
	var forcedOverwrite bool

	verify := func(before, after []byte) error {
		if forcedOverwrite {
			return nil
		}
		return applyVerify(before, after, b.Name)
	}

	return writePolicy(ctx, env, "apply "+b.Name, func(current []byte) ([]byte, error) {
		res, err := policy.Apply(current, b.Data, b.Name, opts)
		if err != nil {
			return nil, err
		}
		if len(res.Conflicts) > 0 {
			for _, c := range res.Conflicts {
				fmt.Fprintf(env.Out, "conflict at %s\n  bundle:               %s\n  currently in your policy: %s\n", c.Path, c.Incoming, c.Existing)
			}
			if res.Policy == nil {
				return nil, fmt.Errorf("%d conflict(s); nothing was changed. Re-run with --force to overwrite, or --skip-conflicts to install the rest", len(res.Conflicts))
			}
			if opts.Force {
				forcedOverwrite = true
				fmt.Fprintf(env.Out, "warning: --force overwrote %d existing value(s) shown above. "+
					"scurgery does not mark what it replaced, so `scurgery remove` cannot restore it; "+
					"the backup taken before this write is the only copy of the original. "+
					"scurgery's local self-check does not run on a forced overwrite, so review the diff above before confirming.\n", len(res.Conflicts))
			}
		}
		fmt.Fprintf(env.Out, "adding %d, skipping %d already present\n", res.Added, res.Skipped)
		return res.Policy, nil
	}, verify)
}
