//go:build !windows

package sys

// CleanStartupApproved is a no-op on non-Windows platforms.
func CleanStartupApproved(_ ...string) error {
	return nil
}
