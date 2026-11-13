# Hostup Architecture

## Overview

Hostup is a native-first Linux management application written in Go.

## Principles

1. CLI and TUI share the same application logic.
2. Linux system tools remain the source of truth.
3. No mandatory background services.
4. Operations should be idempotent where possible.
5. Privileged operations require explicit authorization.
6. Network and SSH changes must protect remote access.
7. Platform-specific behavior must be isolated where necessary.

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
