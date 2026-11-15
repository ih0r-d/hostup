# Hostup Architecture

## Overview

Hostup is a safe, terminal-first Linux operations toolkit written in Go.

All application sources live in `src/`. `src/main.go` runs the Cobra root command
and translates errors into process exit status. `src/internal/` separates CLI,
the reusable setup engine, native system operations, and the TUI. Embedded brand
resources live in `src/assets/`. Build and Multipass tooling lives in `.dev/`;
the root `Taskfile.yml` composes the development and CI workflows.

## Principles

1. CLI and TUI share the same application logic.
2. Linux system tools remain the source of truth.
3. No mandatory background services.
4. Operations should be idempotent where possible.
5. Privileged operations require explicit authorization.
6. Network and SSH changes must protect remote access.
7. Platform-specific behavior must be isolated where necessary.

## Current operation lifecycle

The existing setup engine coordinates read-only inspection/planning, validation,
stale-plan checks, ordered native execution, and post-command verification.
Cobra owns flags, approval, and output; the wizard owns interaction and renders
the same engine results/progress. Neither presentation layer reimplements setup
execution. Native setup reads and process execution remain in `setup/native.go`.
The function dependencies are sufficient for testing; no workflow framework or
additional operation interfaces are needed.

`Result.Executed` records successful commands; `Result.Applied` records settings
confirmed by a subsequent state snapshot. Verification checks both applied and
retained settings and stops on mismatch or unavailable inspection. Errors retain
their causes for `errors.Is`/`errors.As`, including cancellation and permission
errors. Failed commands may have side effects; pending changes were not attempted.
Retries reinspect state. No rollback, atomicity, or persistence is implied.

The service backend remains separate. It has bounded finite operations and
explicit CLI approval for changes; it does not implement setup plans or promise
state verification. Existing read-only diagnostics, TUI screens, installation,
and completion keep their current responsibilities.

## Network inspection

`src/internal/network` owns a shared read-only snapshot model and native collector.
Cobra's `network status/interfaces/routes/dns` commands format that model; the
existing Network TUI screen collects it asynchronously and supports manual refresh.
Rendering performs no native reads. Private function dependencies enable fixture
tests without Linux, root privileges, external packages, or network access.

Go interface APIs, sysfs link metadata, procfs routing views, and `resolv.conf`
are the only sources. Each source carries availability/error status, so missing
DNS or routes retain the remaining snapshot. Only unsupported platforms and
context cancellation/deadlines fail collection globally. Ordering is deterministic;
resolver/search priority remains in configured order. No timestamps, monitoring,
configuration persistence, connectivity claims, or policy-routing completeness
are implied. See the README for source limitations.

## Planned Components

- CLI: command-line interface
- TUI: interactive terminal interface
- Platform: operating system and architecture detection
- Modules: Linux system operations
- Diagnostics: system health checks

## Implementation Strategy

Build features incrementally.

Avoid speculative abstractions and empty packages.

Initial target: Ubuntu Server on ARM64 and AMD64.
