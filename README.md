# scurgery

scurgery adds a named set of blocks to a Tailscale tailnet policy file and
later removes exactly those blocks again, leaving everything else in the
file byte-identical to what it was.

## Why it exists

The Tailscale policy file API is whole-file: you `GET` the whole thing, and
you `POST` the whole thing back. There is no endpoint for "add this grant"
or "remove that grant". Every tool that touches the policy from outside the
admin console has to deal with that somehow, and the existing options all
deal with it by making you own more than you should have to:

| Tool | Model | Why it does not solve this |
|---|---|---|
| `tailscale_acl` (Terraform, Pulumi) | Whole-file resource | Documented to completely overwrite the existing policy file contents. There is no `If-Match` precondition to satisfy, only an `overwrite_existing_content` flag to disable |
| `gitops-pusher`, `gitops-acl-action` | Git repo is source of truth | Assumes you own the whole policy file |
| `tailscale-acl-combiner` | Merges parent and child files before upload | Still uploads a whole file. Top-level objects and arrays are appended, not merged. No removal, no awareness of live state |
| Visual policy editor | Human GUI | Manual, which is the step you're trying to skip |

scurgery is for the case where a project needs to add a handful of grants,
SSH rules, or tag owners to a tailnet whose policy someone else owns, and
needs to be able to take those additions back out later without touching
anything else in the file, including formatting and comments it didn't add.

## Install

```
go install github.com/nopoz/scurgery/cmd/scurgery@latest
```

Requires Go 1.26.

## Quick start

```
export TS_API_KEY=tskey-api-...
export TS_TAILNET=example.com

scurgery diff examples/aws-subnet-router.hujson
scurgery apply examples/aws-subnet-router.hujson
scurgery remove aws-subnet-router
```

`diff` shows what `apply` would do without writing anything. The bundle's
namespace, `aws-subnet-router`, comes from its filename, and that's what you
pass to `remove` later.

## How it tracks its own blocks

scurgery has no server-side state to work with, so it tracks what it added
using ordinary comments in the policy file itself:

```
// scurgery:aws-subnet-router
{"src": ["tag:aws-app"], "dst": ["tag:aws-subnet-router"], "ip": ["443"]},
```

marks a member or array element added inside a container the operator
already owned. It's removed on its own; the container stays.

```
// scurgery:aws-subnet-router owns-key
"nodeAttrs": [{"target": ["tag:aws-app"], "attr": ["funnel"]}],
```

marks a top-level key scurgery created because it didn't exist yet. On
removal the whole key goes with it, not just its contents, since nothing
else was there to begin with.

These are plain HuJSON comments, and the Tailscale admin console's visual
policy editor preserves comments when it rewrites the file, so ordinary use
through the console does not break scurgery's tracking. Editing the file by
hand and deleting the comments will.

## Safety

Every write runs the same ladder, in order:

1. Read the current policy, capturing its ETag
2. Refuse to proceed if the tailnet returned no ETag, since a write could
   not be conditioned safely
3. Compute the new policy
4. Self-check: strip scurgery's own markers back out of the result and
   confirm what's left is identical to the input, i.e. that scurgery only
   touched what it owns
5. Ask the tailnet to validate the result, requiring an empty response body
   rather than trusting a `200`
6. Render a diff for the operator to read
7. Back up the current policy to a local file
8. Ask for confirmation
9. Write, conditional on the ETag from step 1

If the ETag no longer matches at step 9, meaning something else changed the
policy in the meantime, scurgery aborts and reports that rather than
retrying blind. Re-run scurgery to pick up the new state.

A conflict, where a bundle member or value already exists in the policy
with something different, stops everything by default: nothing is written,
and both values are printed. `--force` and `--skip-conflicts` are the
deliberate ways past that; see Limitations below for what `--force` gives
up.

`status` reports the namespaces it finds by marker. A policy whose marker
comments were stripped reports nothing installed, which is accurate: if
scurgery can't find a marker, it also can't remove anything by marker.

A backup of the policy is written to the working directory, or to
`--backup-dir`, before every write attempt, including a run where the
operator is then asked to confirm and says no. The backup is there whether
or not the write happens.

## Writing a bundle

A bundle is a partial policy file: the same shape as a real policy, holding
only the keys you want to add.

- The namespace is the bundle's filename stem (`aws-subnet-router.hujson`
  becomes `aws-subnet-router`) unless you pass `--name`.
- Where a top-level key already exists in the policy, array elements are
  appended and object members are merged in by key. Where it doesn't exist,
  scurgery creates it.

See `examples/aws-subnet-router.hujson` for a complete bundle: it grants
access to a private VPC subnet through a subnet router and an app node,
allows SSH to both, auto-approves the VPC route, and owns the two tags
involved.

## Limitations

**`--force` is not reversible.** When it overwrites a value already in the
policy, scurgery does not mark what it replaced, so `remove` cannot bring
the original back. The backup taken before the write is the only copy of
what was there. On top of that, scurgery's own self-check (step 4 above)
does not run on a forced overwrite, because it has no way to tell an
authorized overwrite apart from accidental damage to content it doesn't
own. The diff shown at step 6 is the real safeguard for a forced apply:
read it before confirming.

**Two bundles sharing a container scurgery created cannot both be cleanly
removed in one pass.** Say bundle A creates a top-level key that didn't
exist before, and bundle B later adds its own entries into that same key.
Removing A first does nothing: A's rules stay live, and scurgery refuses
the write and explains why. Remove B first, then re-run the removal of A,
and it completes. This happens because scurgery marks the container's key
as owned by A, but has no way to also track that B has since added members
inside it.

**A member two bundles both declare belongs to whichever one installed it
first.** If bundle A and bundle B both contribute a member with the same
value, only the first apply marks it; the second apply finds it already
present and skips it without marking it for itself. `apply` prints a note
naming the member and the namespace that actually owns it when this
happens. Removing the owning namespace later removes that member too, even
though the other bundle also declares it, and removing the other namespace
never does.

**Structural removal (`--match-structural`) is a recovery path, not an
equal alternative.** It's for when the marker comments are gone. Instead of
reading markers, it matches the bundle's members and elements against the
policy by value and removes what matches. Because of that:

- It cannot recognise a block the operator has since edited. Rather than
  guess, it reports those as not found and leaves them alone.
- It leaves an empty container behind for a top-level key scurgery
  originally created, since without a marker it has no way to know that
  key was scurgery's to remove.
- Its self-check is weaker than the normal one. It can't detect
  over-removal of content the bundle itself also describes, such as two
  identical array elements where the bundle names one of them: nothing
  distinguishes which one was meant to survive.

**No OAuth.** Only Tailscale API access tokens are supported.

**No Terraform provider.**

**Duplicate identical array elements are matched by value.** Two members of
an array (say `grants`) that are semantically identical are indistinguishable
to scurgery; it cannot tell them apart when deciding what to skip, mark, or
remove.
