package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// currentName is the name of the symlink that points at the active version.
// It is reserved and cannot be used as a version name.
const currentName = "current"

// binLinkStatus describes what currently occupies a path in the bin directory.
type binLinkStatus int

const (
	// binLinkAbsent means nothing occupies the path.
	binLinkAbsent binLinkStatus = iota
	// binLinkManaged means the path is a symlink pointing inside baseDir.
	binLinkManaged
	// binLinkForeign means the path is a symlink pointing outside baseDir,
	// e.g. one the user created by hand.
	binLinkForeign
	// binLinkRegularFile means the path is a real file, e.g. a binary installed
	// by hand before adopting lav.
	binLinkRegularFile
	// binLinkOther means the path is a directory or something else entirely.
	binLinkOther
)

// appLinkReport records what happened while making an app reachable from binDir.
type appLinkReport struct {
	// Linked lists commands whose symlink was created or repointed.
	Linked []string
	// Unchanged lists commands that already pointed at the right target.
	Unchanged []string
	// Dangling lists lav-created symlinks that no longer resolve.
	Dangling []string
	// Pruned lists dangling symlinks that were removed.
	Pruned []string
	// Repaired is the file that was renamed to bin/<app> to fix a legacy
	// layout, or "" when no repair was needed or possible.
	Repaired string
	// Errs collects per-command failures. One failing command does not stop
	// the others.
	Errs []error
}

// Warnings renders everything the caller should tell the user about, in the
// order it happened.
func (r appLinkReport) Warnings(app string) []string {
	var out []string
	for _, err := range r.Errs {
		out = append(out, err.Error())
	}
	for _, name := range r.Dangling {
		out = append(out, fmt.Sprintf("%s is a broken lav symlink; run 'lav link %s' to repair it", name, app))
	}
	return out
}

// isUnder reports whether path is parent itself or lies inside it. Both are
// expected to be absolute and cleaned.
func isUnder(parent, path string) bool {
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// resolveLinkTarget turns a symlink target into an absolute, cleaned path
// without touching the filesystem. linkDir is the directory holding the link.
func resolveLinkTarget(linkDir, target string) string {
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	return filepath.Clean(filepath.Join(linkDir, target))
}

// binLinkTarget builds the symlink target for <binDir>/<name>, pointing at
// <baseDir>/<app>/current/bin/<name>.
//
// A relative target is preferred so the whole tree can be relocated, which for
// the default layout yields the historical ../share/lav/<app>/current/bin/<name>.
// filepath.Rel only fails for paths that share no root (different volumes on
// Windows), in which case an absolute target still works.
func binLinkTarget(binDir, baseDir, app, name string) string {
	abs := filepath.Join(baseDir, app, currentName, "bin", name)
	if rel, err := filepath.Rel(binDir, abs); err == nil {
		return rel
	}
	return abs
}

// classifyBinLink reports what occupies linkPath, along with the absolute path
// its target resolves to when it is a symlink.
func classifyBinLink(baseDir, linkPath string) (binLinkStatus, string, error) {
	info, err := os.Lstat(linkPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return binLinkAbsent, "", nil
		}
		return binLinkOther, "", fmt.Errorf("failed to inspect %s: %w", linkPath, err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(linkPath)
		if err != nil {
			return binLinkOther, "", fmt.Errorf("failed to read symlink %s: %w", linkPath, err)
		}
		resolved := resolveLinkTarget(filepath.Dir(linkPath), target)
		if isUnder(baseDir, resolved) {
			return binLinkManaged, resolved, nil
		}
		return binLinkForeign, resolved, nil
	}

	if info.Mode().IsRegular() {
		return binLinkRegularFile, "", nil
	}
	return binLinkOther, "", nil
}

// checkBinLink reports why <binDir>/<name> cannot be turned into a lav-managed
// symlink, or nil when it can. It never writes anything, so callers can use it
// to validate before mutating any state.
func checkBinLink(binDir, baseDir, name string, force bool) error {
	linkPath := filepath.Join(binDir, name)
	status, target, err := classifyBinLink(baseDir, linkPath)
	if err != nil {
		return err
	}

	switch status {
	case binLinkAbsent, binLinkManaged:
		return nil
	case binLinkRegularFile:
		if force {
			return nil
		}
		return fmt.Errorf("%s exists and is not a symlink; remove it and re-run, or pass --force to replace it", linkPath)
	case binLinkForeign:
		if force {
			return nil
		}
		return fmt.Errorf("%s is a symlink to %s, which lav does not manage; remove it and re-run, or pass --force to replace it", linkPath, target)
	default:
		return fmt.Errorf("%s exists and is neither a regular file nor a symlink; remove it manually and re-run", linkPath)
	}
}

// checkBinLinks validates every name up front and reports all the problems at
// once, so a conflict cannot surface only after part of an install is done.
func checkBinLinks(binDir, baseDir string, names []string, force bool) error {
	var errs []error
	for _, name := range names {
		if err := checkBinLink(binDir, baseDir, name, force); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// replaceSymlink points linkPath at target, replacing whatever is already
// there.
//
// The new link is built next to linkPath and renamed over it: rename is atomic
// and replaces a regular file, so an existing file is never deleted before its
// replacement exists. Renaming onto a directory fails, which keeps directories
// safe by construction.
func replaceSymlink(linkPath, target string) error {
	dir := filepath.Dir(linkPath)
	tmp := filepath.Join(dir, "."+filepath.Base(linkPath)+".lav-tmp")

	if err := os.Remove(tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to clear %s: %w", tmp, err)
	}
	if err := os.Symlink(target, tmp); err != nil {
		return fmt.Errorf("failed to create symlink %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, linkPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("failed to move symlink into place at %s: %w", linkPath, err)
	}
	return nil
}

// ensureBinLink makes <binDir>/<name> a symlink to the app's current bin entry.
// It returns whether the link had to be written.
func ensureBinLink(binDir, baseDir, app, name string, force bool) (bool, error) {
	if err := checkBinLink(binDir, baseDir, name, force); err != nil {
		return false, err
	}

	linkPath := filepath.Join(binDir, name)
	target := binLinkTarget(binDir, baseDir, app, name)

	status, resolved, err := classifyBinLink(baseDir, linkPath)
	if err != nil {
		return false, err
	}
	if status == binLinkManaged && resolved == filepath.Join(baseDir, app, currentName, "bin", name) {
		return false, nil
	}

	if err := os.MkdirAll(binDir, 0755); err != nil {
		return false, fmt.Errorf("failed to create %s: %w", binDir, err)
	}
	if err := replaceSymlink(linkPath, target); err != nil {
		return false, err
	}
	return true, nil
}

// linkBinNames links every name, collecting failures instead of stopping at the
// first one: a package with several binaries must not end up half linked
// without saying so.
func linkBinNames(binDir, baseDir, app string, names []string, force bool) appLinkReport {
	var report appLinkReport
	for _, name := range names {
		written, err := ensureBinLink(binDir, baseDir, app, name, force)
		switch {
		case err != nil:
			report.Errs = append(report.Errs, err)
		case written:
			report.Linked = append(report.Linked, name)
		default:
			report.Unchanged = append(report.Unchanged, name)
		}
	}
	return report
}

// commandNames lists the executables in a bin directory. Symlinks are followed,
// so a repaired bin/<app> counts, while directories, non-executable files and
// broken links are skipped instead of being linked into the user's PATH.
func commandNames(binSrcDir string) ([]string, error) {
	entries, err := os.ReadDir(binSrcDir)
	if err != nil {
		return nil, err
	}

	var names []string
	for _, entry := range entries {
		info, err := os.Stat(filepath.Join(binSrcDir, entry.Name()))
		if err != nil {
			continue
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			continue
		}
		names = append(names, entry.Name())
	}

	sort.Strings(names)
	return names, nil
}

// currentBinDir returns the bin directory of the app's current version.
func currentBinDir(baseDir, app string) string {
	return filepath.Join(baseDir, app, currentName, "bin")
}

// danglingAppLinks lists entries in binDir that lav created for app and that no
// longer resolve. Only symlinks pointing inside the app's own directory are
// reported, so nothing the user manages by hand is ever included.
func danglingAppLinks(binDir, baseDir, app string) ([]string, error) {
	entries, err := os.ReadDir(binDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	appDir := filepath.Join(baseDir, app)

	var names []string
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink == 0 {
			continue
		}
		linkPath := filepath.Join(binDir, entry.Name())
		target, err := os.Readlink(linkPath)
		if err != nil {
			continue
		}
		if !isUnder(appDir, resolveLinkTarget(binDir, target)) {
			continue
		}
		if _, err := os.Stat(linkPath); errors.Is(err, fs.ErrNotExist) {
			names = append(names, entry.Name())
		}
	}

	sort.Strings(names)
	return names, nil
}

// repairLegacyBinName fixes an app installed by an older lav, which stored the
// binary under its source filename (myapp-1.2.0-x86_64.AppImage) instead of the
// app name. Such a version has no bin/<app>, so <binDir>/<app> cannot resolve
// through current.
//
// The file is renamed to bin/<app>, which leaves the version laid out exactly
// like one installed by the current lav — a single command named after the app.
// It only acts when the version holds exactly one regular executable, since
// with several there is nothing to infer. The old name is returned, or "" when
// nothing needed or could be done.
func repairLegacyBinName(baseDir, app string) (string, error) {
	version, err := getCurrentVersion(baseDir, app)
	if err != nil {
		return "", err
	}
	if version == "" {
		return "", nil
	}

	binDir := filepath.Join(baseDir, app, version, "bin")
	appPath := filepath.Join(binDir, app)
	if _, err := os.Lstat(appPath); err == nil {
		return "", nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("failed to inspect %s: %w", appPath, err)
	}

	names, err := commandNames(binDir)
	if err != nil || len(names) != 1 {
		return "", nil
	}

	// Only rename a real file: a symlink here is something lav did not put
	// there, and following it would move the wrong thing.
	oldPath := filepath.Join(binDir, names[0])
	info, err := os.Lstat(oldPath)
	if err != nil || !info.Mode().IsRegular() {
		return "", nil
	}

	if err := os.Rename(oldPath, appPath); err != nil {
		return "", fmt.Errorf("failed to rename %s to %s: %w", oldPath, appPath, err)
	}
	return names[0], nil
}

// linkApp makes every command of the app's current version reachable from
// binDir. repair enables the legacy-layout fix and the removal of lav's own
// broken links, which only the explicit `lav link` command asks for.
func linkApp(binDir, baseDir, app string, force, repair bool) (appLinkReport, error) {
	if err := validateAppName(app); err != nil {
		return appLinkReport{}, err
	}

	version, err := getCurrentVersion(baseDir, app)
	if err != nil {
		return appLinkReport{}, err
	}
	if version == "" {
		return appLinkReport{}, fmt.Errorf("no current version set for %s; run 'lav use %s <version>'", app, app)
	}

	var report appLinkReport
	if repair {
		repaired, err := repairLegacyBinName(baseDir, app)
		if err != nil {
			report.Errs = append(report.Errs, err)
		}
		report.Repaired = repaired
	}

	names, err := commandNames(currentBinDir(baseDir, app))
	if err != nil {
		return report, fmt.Errorf("failed to read bin directory of %s %s: %w", app, version, err)
	}
	if len(names) == 0 {
		return report, fmt.Errorf("%s %s contains no executable in bin/", app, version)
	}

	linked := linkBinNames(binDir, baseDir, app, names, force)
	report.Linked = linked.Linked
	report.Unchanged = linked.Unchanged
	report.Errs = append(report.Errs, linked.Errs...)

	dangling, err := danglingAppLinks(binDir, baseDir, app)
	if err != nil {
		report.Errs = append(report.Errs, fmt.Errorf("failed to scan %s: %w", binDir, err))
		return report, nil
	}

	for _, name := range dangling {
		if !repair {
			report.Dangling = append(report.Dangling, name)
			continue
		}
		linkPath := filepath.Join(binDir, name)
		if err := os.Remove(linkPath); err != nil {
			report.Errs = append(report.Errs, fmt.Errorf("failed to remove broken symlink %s: %w", linkPath, err))
			report.Dangling = append(report.Dangling, name)
			continue
		}
		report.Pruned = append(report.Pruned, name)
	}

	return report, nil
}

// useVersion switches the app to a version and keeps binDir in sync, so a
// missing link is recreated instead of being silently left broken.
func useVersion(binDir, baseDir, app, version string, force bool) (appLinkReport, error) {
	if err := switchVersion(baseDir, app, version); err != nil {
		return appLinkReport{}, err
	}
	report, err := linkApp(binDir, baseDir, app, force, false)
	if err != nil {
		report.Errs = append(report.Errs, err)
	}
	return report, nil
}
