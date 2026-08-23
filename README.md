# scurgery

[![CI](https://github.com/nopoz/scurgery/actions/workflows/ci.yml/badge.svg)](https://github.com/nopoz/scurgery/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/nopoz/scurgery?sort=semver)](https://github.com/nopoz/scurgery/releases/latest)
[![Go](https://img.shields.io/github/go-mod/go-version/nopoz/scurgery)](go.mod)
[![Build provenance](https://img.shields.io/badge/build%20provenance-attested-success)](#release-binaries)
[![License](https://img.shields.io/github/license/nopoz/scurgery)](LICENSE)

scurgery adds a named set of blocks to a Tailscale access policy file and
later removes exactly those blocks again, leaving everything else in the
file byte-identical to what it was.

## Contents

- [The problem](#the-problem)
- [Why not an existing tool](#why-not-an-existing-tool)
- [Install](#install)
  - [Release binaries](#release-binaries)
- [Quick start](#quick-start)
- [Credentials](#credentials)
- [How it tracks its own blocks](#how-it-tracks-its-own-blocks)
- [Safety](#safety)
- [Writing a bundle](#writing-a-bundle)
- [Automation](#automation)
  - [Exit codes](#exit-codes)
  - [JSON output](#json-output)
  - [Checking for drift in CI](#checking-for-drift-in-ci)
  - [Terraform](#terraform)
- [Limitations](#limitations)
- [Support](#support)

## The problem

The Tailscale policy file API is whole-file: you `GET` the whole thing, and
you `POST` the whole thing back. There is no endpoint for "add this grant"
or "remove that grant". Every tool that touches the policy from outside the
admin console has to answer that somehow, and the published ones answer it by
taking charge of the whole file, rules other people wrote included.

scurgery is for the case where a project needs to add a handful of grants,
SSH rules, or tag owners to a tailnet whose policy someone else owns, and
needs to be able to take those additions back out later without touching
anything else in the file, including formatting and comments it didn't add.

That case turns up whenever provisioning wants to be one command. A Terraform
module that builds a tagged subnet router needs tag owners, an approved route
and a couple of grants in the policy before its nodes register, and today it
either takes over the operator's whole policy file to put them there or ends
its setup instructions with a block to paste in by hand. scurgery is the third
option: apply the bundle before the stack goes up, remove it after the stack
comes down.

Two things it deliberately is not. It authenticates with Tailscale API access
tokens only, not OAuth. And it is a CLI rather than a Terraform provider: a
configuration cannot declare a bundle as a resource, though the
`terraform_data` pattern under Automation applies and removes one with the
stack.

## Why not an existing tool

None of the published tools can add a few rules now and remove exactly those
rules later, leaving the rest of the file as it was:

| Tool | Model | Why it does not solve this |
|---|---|---|
| `tailscale_acl` (Terraform, Pulumi) | Whole-file resource | Documented to completely overwrite the existing policy file contents. There is no `If-Match` precondition to satisfy, only an `overwrite_existing_content` flag that waives the import-first guard. Destroying the resource either leaves your rules in place or, with `reset_acl_on_destroy`, resets the whole tailnet policy to the default |
| `gitops-pusher`, `gitops-acl-action` | Git repo is source of truth | Assumes you own the whole policy file. Undoing a change means reverting the commit and pushing the whole file again, which also reverts whatever anyone else changed in the meantime |
| `tailscale-acl-combiner` | Merges parent and child files before upload | The result is still a whole file, pushed wholesale by whatever applies it. Top-level objects and arrays are appended, not merged. No removal, no awareness of live state |
| Visual policy editor | Human GUI | Manual, which is the step you're trying to skip, and removal means remembering which lines were yours |

## Install

```
go install github.com/nopoz/scurgery/cmd/scurgery@latest
```

Requires Go 1.26.

`scurgery version` reports the build you are running.

### Release binaries

Each tagged release publishes static binaries for Linux, macOS and Windows on
amd64 and arm64, along with a `checksums.txt` covering every archive. To
install one without a Go toolchain, download the archive for your platform
from the [releases page](https://github.com/nopoz/scurgery/releases), verify
it, and extract it:

```
sha256sum -c checksums.txt --ignore-missing
tar -xzf scurgery_v0.1.0_linux_amd64.tar.gz
```

The archives carry a build provenance attestation, so you can also confirm
which workflow run produced one:

```
gh attestation verify scurgery_v0.1.0_linux_amd64.tar.gz --repo nopoz/scurgery
```

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

`diff` exits 1 if applying the bundle would change the policy, and 0 if
everything in it is already installed, so it works as a CI check for policy
drift.

## Credentials

scurgery reads `TS_API_KEY` and `TS_TAILNET` from the environment. Exporting
them every time defeats the point of a tool meant to be scripted, so it also
reads them from a config file:

```
mkdir -p ~/.config/scurgery
cat > ~/.config/scurgery/config <<'EOF'
TS_API_KEY=tskey-api-...
TS_TAILNET=example.com
EOF
chmod 600 ~/.config/scurgery/config
```

The path is `$XDG_CONFIG_HOME/scurgery/config` when that variable is set, and
`~/.config/scurgery/config` otherwise. Not having the file is fine; it is only
read if it exists.

The format is `KEY=VALUE` lines. Blank lines and lines starting with `#` are
ignored, whitespace around the key and value is trimmed, and one matched pair
of surrounding quotes is stripped. There is no shell expansion and no `export`.

`TS_API_KEY` and `TS_TAILNET` are the only keys the file may set. Any other key
is an error rather than something quietly ignored, which is what keeps this
file from becoming a place to redirect where your token gets sent.

Because the file holds an API token, scurgery refuses to read it if anyone but
the owner can, and tells you to `chmod 600` it.

Precedence runs `--tailnet` first, then the environment, then the file. The
environment beating the file is deliberate: CI and Terraform supply secrets as
environment variables, and a config file on a build agent must not displace
them.

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
and both values are printed. That's the rule when the existing value
belongs to the operator or to another namespace. When it's scurgery's own
previous value for the namespace being applied, there's no conflict: it's
updated in place instead. See Limitations below for what that means for a
hand-edit made to a scurgery-installed value. `--force` and
`--skip-conflicts` are the deliberate ways past a real conflict; see
Limitations below for what `--force` gives up.

`status` reports the namespaces it finds by marker. A policy whose marker
comments were stripped reports nothing installed, which is accurate: if
scurgery can't find a marker, it also can't remove anything by marker.

A backup of the policy is written to the working directory, or to
`--backup-dir`, before every write attempt, including a run where the
operator is then asked to confirm and says no. The backup is there whether
or not the write happens.

A backup file holds the complete tailnet policy: every hostname, subnet,
tag, and group membership in it, not just what scurgery is about to touch.
The default `--backup-dir` is the working directory, so running scurgery
inside a project repository leaves that file sitting there to be
accidentally committed. `policy-backup-*.hujson` is in this repository's
own `.gitignore`; if you run scurgery from inside a repository of your own,
add the same pattern to yours, or point `--backup-dir` somewhere outside
version control.

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

## Automation

`apply` and `remove` take `--yes`, so nothing waits for a human.

### Exit codes

Exit codes are a contract:

| Code | Meaning |
|---|---|
| 0 | Success. For `diff`, nothing would change |
| 1 | `diff` only: applying the bundle would change the policy |
| 2 | Usage error |
| 3 | Runtime error: credentials, network, or a refused write |

A runtime failure is deliberately not 1. A CI gate that reads an expired
token as "the policy needs updating" is worse than one that reads nothing at
all.

### JSON output

`status` and `diff` take `--json`, which puts one JSON document on stdout and
nothing else. Everything human-readable goes to stderr instead, including the
per-conflict detail lines, so nothing is lost by asking for JSON. A failure
prints its message and emits no document at all.

```
$ scurgery status --json
{"installed":["aws-router"]}

$ scurgery diff aws-router.hujson --json
{"bundle":"aws-router","changed":true,"diff":"  {\n+  \"tagOwners\": {\n..."}
```

`--json` is rejected on `apply` and `remove` rather than accepted and
ignored, so nothing can quietly believe it got a machine-readable result.

### Checking for drift in CI

`diff` prints what would change and writes nothing, so it works as a gate. Its
exit code says which of the three outcomes above happened, which is what lets a
script tell "the policy needs updating" apart from "the check itself failed":

```sh
scurgery diff policy.hujson
case $? in
  0) ;;                                          # up to date
  1) echo "Policy needs updating."; exit 1 ;;
  *) echo "Check failed. See the error above."; exit 1 ;;
esac
```

### Terraform

There is no scurgery provider. The bundle goes up and comes down with the
stack through a `terraform_data` resource:

```hcl
resource "terraform_data" "policy" {
  input = "aws-router"

  provisioner "local-exec" {
    command = "scurgery apply ${path.module}/policy.hujson --name ${self.input} --yes"
  }

  provisioner "local-exec" {
    when    = destroy
    command = "scurgery remove ${self.input} --yes"
  }
}

resource "aws_instance" "router" {
  depends_on = [terraform_data.policy]
  # ...
}
```

Three things to know:

- A destroy-time provisioner may only reference `self`, `count` and `each`.
  That is why the namespace is carried in `input` rather than a variable.
- `TS_API_KEY` and `TS_TAILNET` come from the environment Terraform itself
  runs in, or from an `environment` block on the provisioner. Prefer the
  first: a token in an `environment` block ends up in the plan file. The
  config file works here too, and the environment still wins over it.
- Provisioners run outside the plan, so `terraform plan` will not show the
  policy change. Use `scurgery diff` for that.

A working example of this pattern:
[tailscale-subnet-router-aws](https://github.com/nopoz/tailscale-subnet-router-aws)
applies a bundle of tag owners, an approved route and a couple of grants before
its nodes come up, and removes them when the stack is destroyed.

## Limitations

| Limitation | When it bites |
|---|---|
| Re-applying a bundle overwrites scurgery's own previous value | You hand-edited a value scurgery installed, then apply that bundle again |
| `--force` cannot be undone | You overwrite a value scurgery does not own |
| Two bundles sharing a container scurgery created | Bundle A creates a top-level key, bundle B adds into it, and you remove A first |
| A shared member belongs to whichever bundle installed it first | Two bundles declare a member with the same value |
| `--match-structural` gives weaker guarantees | The marker comments are gone, so removal matches by value |
| Duplicate identical array elements cannot be told apart | One array holds two semantically identical entries |

**Re-applying a bundle overwrites scurgery's own previous value, including a
hand-edit.** Applying a namespace again with a changed value updates the
previously-installed value in place rather than refusing, since that value is
already marked as belonging to that namespace. scurgery cannot tell a
deliberate hand-edit apart from the value it left there itself, so no conflict
is raised. The rendered diff and the confirmation prompt are what stand between
that and the tailnet, so read the diff before confirming. This affects object
members only: array elements merge by append, so a changed entry is added
alongside the old one and both stay live until one is removed.

**`--force` is not reversible.** scurgery does not mark what it replaced, so
`remove` cannot bring the original back, and the backup taken before the write
is the only copy of what was there. The self-check (step 4 above) is also
skipped on a forced overwrite, because scurgery has no way to tell an
authorized overwrite apart from accidental damage to content it does not own.
The diff at step 6 is the real safeguard for a forced apply: read it before
confirming.

**Two bundles sharing a container scurgery created cannot both be cleanly
removed in one pass.** If bundle A creates a top-level key that did not exist
before and bundle B later adds its own entries into that same key, removing A
first does nothing: A's rules stay live, and scurgery refuses the write and
explains why. Remove B first, then re-run the removal of A, and it completes.
scurgery marks the container's key as owned by A, but has no way to also track
that B has since added members inside it.

**A member two bundles both declare belongs to whichever one installed it
first.** The second apply finds the member already present and skips it without
marking it for itself. When that member carries its own marker, `apply` prints
a note naming it and the namespace that actually owns it. It cannot do that
when the member lives inside a top-level container the first bundle created
wholesale, since nothing inside such a container is individually marked, so
there is no owner to name and nothing is printed.
Removing the owning namespace later removes that member too, even though the
other bundle also declares it, and removing the other namespace never does. In
the wholesale-container case the rendered diff is the first the operator hears
of it, before they confirm the removal.

**Structural removal (`--match-structural`) is a recovery path, not an equal
alternative.** It is for when the marker comments are gone. Instead of reading
markers, it matches the bundle's members and elements against the policy by
value and removes what matches. Because of that:

- It cannot recognise a block the operator has since edited. Rather than
  guess, it reports those as not found and leaves them alone.
- It leaves an empty container behind for a top-level key scurgery
  originally created, since without a marker it has no way to know that
  key was scurgery's to remove.
- Its self-check is weaker than the normal one. It can't detect
  over-removal of content the bundle itself also describes, such as two
  identical array elements where the bundle names one of them: nothing
  distinguishes which one was meant to survive.

**Duplicate identical array elements are matched by value.** Two elements of an
array (say `grants`) that are semantically identical are indistinguishable to
scurgery when it decides what to skip, mark, or remove.

## Support

If you like this project you can show your support by giving it a star and also
on [tailscale-subnet-router-aws](https://github.com/nopoz/tailscale-subnet-router-aws),
the Terraform configuration this was written for.

Questions and bug reports are welcome. [Open an issue](https://github.com/nopoz/scurgery/issues),
and include the output of `scurgery version` and the command you ran.

Check out some of my other projects:

- [pfsense-dnscrypt-proxy](https://github.com/nopoz/pfsense-dnscrypt-proxy): encrypted DNS on pfSense, with full GUI support
- [portrieve](https://github.com/nopoz/portrieve): back up, restore and migrate Portainer stacks as plain Docker Compose files
- [hosaka](https://github.com/nopoz/hosaka): a Docker image update monitor, with notifications and one-click updates

The rest are at [github.com/nopoz](https://github.com/nopoz).
