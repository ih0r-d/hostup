//go:build !linux

package cli

import "os"

func isTerminalFile(file *os.File) bool { return false }
