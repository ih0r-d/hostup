# Hostup

**A lightweight, native-first Linux setup and management toolkit.**

Hostup aims to simplify Linux system configuration,
diagnostics, and everyday management through a unified
CLI and interactive terminal interface.

## Philosophy

- Native Linux tools first
- No mandatory background daemons
- No Docker or Ansible dependencies
- Minimal resource consumption
- Predictable and transparent operations
- Safe and repeatable configuration
- CLI and TUI backed by shared functionality

## Planned Platforms

- Ubuntu Server
- Debian
- Raspberry Pi OS

Target architectures: ARM64 and AMD64.

## Planned Features

- Initial system setup
- System diagnostics
- Network configuration
- SSH configuration
- VPN management
- systemd service management
- Storage management
- System updates and maintenance

## Development

Requirements:

- Go 1.24+
- Task (optional)

Build:

    go build -o bin/hostup ./cmd/hostup

Run:

    ./bin/hostup

Test:

    go test ./...

Task commands:

    task --list

## Project Status

Early development.

The current CLI is a minimal bootstrap.
System management features are not implemented yet.

## Documentation

Existing Raspberry Pi guides are preserved under:

`docs/guides/raspberry-pi/`

These are legacy reference materials and may require updates.

## License

To be determined.
