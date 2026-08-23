# Batch 12 — Packaging, onboarding and first users

**Depends on:** everything
**Blocks:** shipping
**Packages:** `cmd/assay`, plus the UI repo

## Goal

Turn the engine into something a stranger downloads and uses without contacting
you. This is the batch where the product stops being a codebase.

## Deliverables

- `cmd/assay` serves the ConnectRPC API and the static UI on localhost, picks a
  free port, and opens a browser
- Single static binary per platform, `CGO_ENABLED=0`, cross-compiled for macOS
  (arm64 and amd64) and Windows
- Optional native wrapper — evaluated, not assumed. If it costs more than a day,
  ship the localhost version and revisit
- In-app data manager: browse instruments, see quality tiers, fetch with
  streaming progress, resume interrupted downloads
- Near-miss view wired to the batch 07 data
- Onboarding: first-run flow that fetches a small dataset and opens a worked
  example rather than an empty builder
- **Five forkable example strategies** — trend pullback, mean reversion,
  breakout, SMC, session — each with a written explanation of what it tests
- `docs/primitives.md` complete: every primitive's exact definition and every
  parameter's meaning
- Crash reporting that is local-only and opt-in, or none at all

## Key decisions

**Localhost server first, native wrapper second.** The wrapper is packaging
polish. Making it load-bearing risks the release on a dependency that has nothing
to do with the product's value.

**Onboarding opens a working example, not a blank builder.** Five empty slots is
still a cold start even with a checklist. A user who forks a working strategy and
changes one parameter has understood the tool; a user staring at empty dropdowns
has not.

**Documentation of primitive definitions is a shipping requirement.** The whole
positioning rests on users being able to check what the tool means by an order
block. Undocumented detectors make the product exactly the black box it claims
not to be.

**No telemetry by default.** A tool whose pitch is honesty and local-first
control does not phone home. If usage data is needed later, ask for it
explicitly.

## Tests

- Cross-compilation matrix builds clean with `CGO_ENABLED=0`
- Fresh-machine test in a clean container: download binary, launch, complete a
  study, with no prior state and no manual steps
- Port selection handles an occupied port
- Interrupted download resumes correctly
- Each of the five example strategies runs to completion on the seeded library

## Exit criteria

- [ ] A stranger downloads one file, fetches data, and completes a study without
      contacting you — verified with at least three people who have not seen the
      project
- [ ] All five examples run on a fresh install
- [ ] `docs/primitives.md` covers every registered primitive
- [ ] Binaries are static and unsigned-but-runnable on each target, with
      documented instructions for the macOS Gatekeeper prompt

## Out of scope

Auto-update. Installers. Code signing — document the workaround for v0 and
revisit once there are users worth the certificate cost. Accounts, hosting,
payments.

## Decision log

_Record the wrapper decision and its cost, and the platform matrix actually shipped._
