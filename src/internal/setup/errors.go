package setup

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidConfiguration    = errors.New("invalid setup configuration")
	ErrUnsupportedPlatform     = errors.New("unsupported setup platform")
	ErrUnknownState            = errors.New("setup state is unknown")
	ErrStalePlan               = errors.New("stale setup plan")
	ErrVerificationMismatch    = errors.New("setup verification mismatch")
	ErrVerificationUnavailable = errors.New("setup verification unavailable")
)

// VerificationError distinguishes a reliable mismatch (Cause == nil) from an
// unavailable observation. A successful command alone never establishes success.
type VerificationError struct {
	Setting  string
	Expected string
	Actual   string
	Cause    error
}

func (e *VerificationError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("verify %s: resulting state unavailable: %v", e.Setting, e.Cause)
	}
	return fmt.Sprintf("verify %s: expected %q, observed %q", e.Setting, e.Expected, e.Actual)
}
func (e *VerificationError) Unwrap() error { return e.Cause }
func (e *VerificationError) Is(target error) bool {
	return (e.Cause == nil && target == ErrVerificationMismatch) || (e.Cause != nil && target == ErrVerificationUnavailable)
}
