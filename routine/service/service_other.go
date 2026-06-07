//go:build !darwin && !linux && !windows

package service

import (
	"context"
	"fmt"
	"io"
	"runtime"
)

// New returns a stub service for platforms without a real installer
// (everything outside darwin/linux/windows — e.g. freebsd, openbsd). The
// stub satisfies the interface so the CLI doesn't need build tags at every
// call site.
func New() Service { return unsupportedService{platform: runtime.GOOS} }

type unsupportedService struct {
	platform string
}

func (u unsupportedService) Install(string, InstallOptions) error {
	return wrapUnsupported(u.platform)
}

func (u unsupportedService) Uninstall() error { return wrapUnsupported(u.platform) }

func (u unsupportedService) Status() (Status, error) {
	return Status{}, wrapUnsupported(u.platform)
}

func (u unsupportedService) TailLogs(context.Context, io.Writer, bool) error {
	return wrapUnsupported(u.platform)
}

func wrapUnsupported(platform string) error {
	return fmt.Errorf("%w (%s)", ErrUnsupported, platform)
}
