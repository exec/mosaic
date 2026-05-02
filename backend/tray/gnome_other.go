//go:build !linux

package tray

import "context"

// AppIndicatorExtensionUUID is exported for build-tag symmetry; on
// non-Linux platforms it has no effect.
const AppIndicatorExtensionUUID = "appindicatorsupport@rgcjonas.gmail.com"

// GnomePromptStatus mirrors the Linux-side type.
type GnomePromptStatus string

const (
	GnomePromptStatusNotApplicable GnomePromptStatus = "not_applicable"
	GnomePromptStatusNeedsInstall  GnomePromptStatus = "needs_install"
	GnomePromptStatusNeedsEnable   GnomePromptStatus = "needs_enable"
	GnomePromptStatusNeedsRestart  GnomePromptStatus = "needs_restart"
)

func IsGnomeSession() bool                                          { return false }
func AppIndicatorExtensionInstalled() bool                          { return false }
func AppIndicatorExtensionEnabled(ctx context.Context) bool         { return false }
func EnableAppIndicatorExtension(ctx context.Context) error         { return nil }
func EvaluateGnomePromptStatus(ctx context.Context) GnomePromptStatus {
	return GnomePromptStatusNotApplicable
}
