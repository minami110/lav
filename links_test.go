package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// newDirs returns a bin directory and a lav base directory laid out like the
// real ~/.local/bin and ~/.local/share/lav, but under a temp root so no test
// ever writes into the developer's home.
func newDirs(t *testing.T) (binDir, baseDir string) {
	t.Helper()

	root := t.TempDir()
	binDir = filepath.Join(root, "bin")
	baseDir = filepath.Join(root, "share", "lav")
	for _, dir := range []string{binDir, baseDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}
	return binDir, baseDir
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

// sourceBinary writes a release asset whose filename carries the version, the
// shape that issue #3 is about.
func sourceBinary(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	writeExecutable(t, path, content)
	return path
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read %s: %v", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

// assertLinkResolves checks that linkPath is a symlink that actually resolves
// to wantFile. Creating a symlink always succeeds, so only following it proves
// the command is reachable.
func assertLinkResolves(t *testing.T, linkPath, wantFile string) {
	t.Helper()

	info, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("expected a symlink at %s: %v", linkPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not a symlink", linkPath)
	}

	got, err := filepath.EvalSymlinks(linkPath)
	if err != nil {
		t.Fatalf("%s does not resolve: %v", linkPath, err)
	}
	want, err := filepath.EvalSymlinks(wantFile)
	if err != nil {
		t.Fatalf("%s does not exist: %v", wantFile, err)
	}
	if got != want {
		t.Errorf("%s resolves to %s, want %s", linkPath, got, want)
	}
}

func assertNotExist(t *testing.T, path, why string) {
	t.Helper()

	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s exists, but %s", path, why)
	}
}

// --- issue #3: the link is named after the app, not after the source file ---

func TestInstallBinary_StoresAndLinksUnderAppName(t *testing.T) {
	binDir, baseDir := newDirs(t)
	src := sourceBinary(t, "myapp-1.2.0-x86_64.AppImage", "v1.2.0")

	if err := installBinary(binDir, baseDir, src, "myapp", "1.2.0", false); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	stored := filepath.Join(baseDir, "myapp", "1.2.0", "bin", "myapp")
	if got := dirEntries(t, filepath.Join(baseDir, "myapp", "1.2.0", "bin")); !slices.Equal(got, []string{"myapp"}) {
		t.Errorf("version bin contains %v, want [myapp]", got)
	}
	assertLinkResolves(t, filepath.Join(binDir, "myapp"), stored)
	assertNotExist(t, filepath.Join(binDir, filepath.Base(src)),
		"the source filename must not be used for the link")
}

func TestInstallBinary_SecondVersionKeepsOneLink(t *testing.T) {
	binDir, baseDir := newDirs(t)

	if err := installBinary(binDir, baseDir, sourceBinary(t, "myapp-1.2.0-x86_64.AppImage", "v1.2.0"),
		"myapp", "1.2.0", false); err != nil {
		t.Fatalf("install 1.2.0 failed: %v", err)
	}
	if err := installBinary(binDir, baseDir, sourceBinary(t, "myapp-1.3.0-x86_64.AppImage", "v1.3.0"),
		"myapp", "1.3.0", false); err != nil {
		t.Fatalf("install 1.3.0 failed: %v", err)
	}

	if got := dirEntries(t, binDir); !slices.Equal(got, []string{"myapp"}) {
		t.Errorf("bin dir contains %v, want [myapp]: installing a version must not add a link", got)
	}

	current, err := getCurrentVersion(baseDir, "myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if current != "1.3.0" {
		t.Errorf("current is %s, want 1.3.0", current)
	}
	assertLinkResolves(t, filepath.Join(binDir, "myapp"),
		filepath.Join(baseDir, "myapp", "1.3.0", "bin", "myapp"))
}

func TestInstallBinary_ReinstallingSameVersionIsIdempotent(t *testing.T) {
	binDir, baseDir := newDirs(t)
	src := sourceBinary(t, "myapp", "v1.2.0")

	for i := 0; i < 2; i++ {
		if err := installBinary(binDir, baseDir, src, "myapp", "1.2.0", false); err != nil {
			t.Fatalf("install %d failed: %v", i+1, err)
		}
	}

	if got := dirEntries(t, binDir); !slices.Equal(got, []string{"myapp"}) {
		t.Errorf("bin dir contains %v, want [myapp]", got)
	}
	if got := dirEntries(t, filepath.Join(baseDir, "myapp", "1.2.0", "bin")); !slices.Equal(got, []string{"myapp"}) {
		t.Errorf("version bin contains %v, want [myapp]: a temp file was left behind", got)
	}
}

// --- issue #4: a regular file in the way, and failing after the fact ---

func TestInstallBinary_RegularFileIsRefusedBeforeAnythingChanges(t *testing.T) {
	binDir, baseDir := newDirs(t)
	handInstalled := filepath.Join(binDir, "myapp")
	writeExecutable(t, handInstalled, "hand installed")

	err := installBinary(binDir, baseDir, sourceBinary(t, "myapp-1.2.0", "v1.2.0"), "myapp", "1.2.0", false)
	if err == nil {
		t.Fatal("expected an error when the bin entry is a regular file")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should say how to proceed, got: %v", err)
	}

	// The failure must describe a state that was never entered: no version
	// directory, no current symlink, and the user's binary untouched.
	assertNotExist(t, filepath.Join(baseDir, "myapp"),
		"a failed install must not leave the version behind")

	current, err := getCurrentVersion(baseDir, "myapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if current != "" {
		t.Errorf("current is %q, want empty: a failed install must not switch versions", current)
	}

	content, err := os.ReadFile(handInstalled)
	if err != nil {
		t.Fatalf("the hand-installed binary is gone: %v", err)
	}
	if string(content) != "hand installed" {
		t.Errorf("the hand-installed binary was modified: %q", content)
	}
}

func TestInstallBinary_ForceReplacesRegularFile(t *testing.T) {
	binDir, baseDir := newDirs(t)
	writeExecutable(t, filepath.Join(binDir, "myapp"), "hand installed")

	if err := installBinary(binDir, baseDir, sourceBinary(t, "myapp-1.2.0", "v1.2.0"),
		"myapp", "1.2.0", true); err != nil {
		t.Fatalf("install --force failed: %v", err)
	}

	assertLinkResolves(t, filepath.Join(binDir, "myapp"),
		filepath.Join(baseDir, "myapp", "1.2.0", "bin", "myapp"))
	if got := dirEntries(t, binDir); !slices.Equal(got, []string{"myapp"}) {
		t.Errorf("bin dir contains %v, want [myapp]", got)
	}
}

func TestInstallBinary_ForeignSymlinkIsNotClobbered(t *testing.T) {
	binDir, baseDir := newDirs(t)
	elsewhere := sourceBinary(t, "myapp", "not managed by lav")
	if err := os.Symlink(elsewhere, filepath.Join(binDir, "myapp")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	err := installBinary(binDir, baseDir, sourceBinary(t, "myapp-1.2.0", "v1.2.0"), "myapp", "1.2.0", false)
	if err == nil {
		t.Fatal("expected an error for a symlink lav does not manage")
	}

	target, err := os.Readlink(filepath.Join(binDir, "myapp"))
	if err != nil {
		t.Fatalf("the user's symlink is gone: %v", err)
	}
	if target != elsewhere {
		t.Errorf("the user's symlink now points at %s, want %s", target, elsewhere)
	}

	if err := installBinary(binDir, baseDir, sourceBinary(t, "myapp-1.2.0", "v1.2.0"),
		"myapp", "1.2.0", true); err != nil {
		t.Fatalf("install --force failed: %v", err)
	}
	assertLinkResolves(t, filepath.Join(binDir, "myapp"),
		filepath.Join(baseDir, "myapp", "1.2.0", "bin", "myapp"))
}

func TestInstallDirectory_ReportsEveryConflictAtOnce(t *testing.T) {
	binDir, baseDir := newDirs(t)

	src := t.TempDir()
	writeExecutable(t, filepath.Join(src, "bin", "foo"), "foo")
	writeExecutable(t, filepath.Join(src, "bin", "bar"), "bar")
	writeExecutable(t, filepath.Join(binDir, "foo"), "hand installed foo")
	writeExecutable(t, filepath.Join(binDir, "bar"), "hand installed bar")

	err := installDirectory(binDir, baseDir, src, "pkg", "1.0.0", false)
	if err == nil {
		t.Fatal("expected an error")
	}
	// Stopping at the first conflict hides the rest of the work that will fail.
	for _, name := range []string{"foo", "bar"} {
		if !strings.Contains(err.Error(), filepath.Join(binDir, name)) {
			t.Errorf("error does not mention %s: %v", name, err)
		}
	}
	assertNotExist(t, filepath.Join(baseDir, "pkg"), "a failed install must not leave the version behind")
}

func TestInstallDirectory_LinksEveryExecutable(t *testing.T) {
	binDir, baseDir := newDirs(t)

	src := t.TempDir()
	writeExecutable(t, filepath.Join(src, "bin", "foo"), "foo")
	writeExecutable(t, filepath.Join(src, "bin", "bar"), "bar")
	if err := os.WriteFile(filepath.Join(src, "bin", "README"), []byte("docs"), 0644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}

	if err := installDirectory(binDir, baseDir, src, "pkg", "1.0.0", false); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	if got := dirEntries(t, binDir); !slices.Equal(got, []string{"bar", "foo"}) {
		t.Errorf("bin dir contains %v, want [bar foo]: only executables belong in PATH", got)
	}
	for _, name := range []string{"foo", "bar"} {
		assertLinkResolves(t, filepath.Join(binDir, name),
			filepath.Join(baseDir, "pkg", "1.0.0", "bin", name))
	}
}

func TestInstallBinary_CurrentIsNotASymlink(t *testing.T) {
	binDir, baseDir := newDirs(t)
	if err := os.MkdirAll(filepath.Join(baseDir, "myapp", currentName), 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	err := installBinary(binDir, baseDir, sourceBinary(t, "myapp", "v1.2.0"), "myapp", "1.2.0", false)
	if err == nil {
		t.Fatal("expected an error when current is a real directory")
	}
	if !strings.Contains(err.Error(), "not a symlink") {
		t.Errorf("error should explain what is wrong, got: %v", err)
	}
	assertNotExist(t, filepath.Join(baseDir, "myapp", "1.2.0"),
		"a failed install must not copy the version")
}

// --- issue #5: use must not report success while the command is missing ---

func TestUseVersion_RecreatesMissingLink(t *testing.T) {
	binDir, baseDir := newDirs(t)
	for _, v := range []string{"1.0.0", "2.0.0"} {
		if err := installBinary(binDir, baseDir, sourceBinary(t, "myapp", v), "myapp", v, false); err != nil {
			t.Fatalf("install %s failed: %v", v, err)
		}
	}

	// The link is gone: deleted by hand, or never created because something was
	// in the way.
	if err := os.Remove(filepath.Join(binDir, "myapp")); err != nil {
		t.Fatalf("failed to remove link: %v", err)
	}

	report, err := useVersion(binDir, baseDir, "myapp", "1.0.0", false)
	if err != nil {
		t.Fatalf("use failed: %v", err)
	}
	if warnings := report.Warnings("myapp"); len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	assertLinkResolves(t, filepath.Join(binDir, "myapp"),
		filepath.Join(baseDir, "myapp", "1.0.0", "bin", "myapp"))
}

func TestUseVersion_ReportsUnreachableCommand(t *testing.T) {
	binDir, baseDir := newDirs(t)
	writeExecutable(t, filepath.Join(binDir, "myapp"), "hand installed")
	if err := installBinary(binDir, baseDir, sourceBinary(t, "myapp", "v1.0.0"),
		"myapp", "1.0.0", true); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	// A conflict lav must not resolve on its own reappears in the bin dir.
	if err := os.Remove(filepath.Join(binDir, "myapp")); err != nil {
		t.Fatalf("failed to remove link: %v", err)
	}
	writeExecutable(t, filepath.Join(binDir, "myapp"), "hand installed again")

	report, err := useVersion(binDir, baseDir, "myapp", "1.0.0", false)
	if err != nil {
		t.Fatalf("use failed: %v", err)
	}
	if len(report.Warnings("myapp")) == 0 {
		t.Error("use must not report plain success while the command is not reachable")
	}
}

// TestUseVersion_LegacyVersionIsReported covers switching back to a version
// installed by an older lav, which stored the binary under its source name.
// Nothing links current/bin/myapp there, so the app is unreachable and saying
// so is the whole point of issue #5.
func TestUseVersion_LegacyVersionIsReported(t *testing.T) {
	binDir, baseDir := newDirs(t)
	if err := installBinary(binDir, baseDir, sourceBinary(t, "myapp-1.3.0-x86_64.AppImage", "v1.3.0"),
		"myapp", "1.3.0", false); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	writeExecutable(t, filepath.Join(baseDir, "myapp", "1.2.0", "bin", "myapp-1.2.0-x86_64.AppImage"), "v1.2.0")

	report, err := useVersion(binDir, baseDir, "myapp", "1.2.0", false)
	if err != nil {
		t.Fatalf("use failed: %v", err)
	}
	if !slices.Contains(report.Dangling, "myapp") {
		t.Errorf("dangling links are %v, want myapp to be reported as broken", report.Dangling)
	}
	if len(report.Warnings("myapp")) == 0 {
		t.Error("use must warn when the app is left unreachable")
	}
}

func TestLinkApp_RepairsLegacyLayout(t *testing.T) {
	binDir, baseDir := newDirs(t)
	if err := installBinary(binDir, baseDir, sourceBinary(t, "myapp-1.3.0-x86_64.AppImage", "v1.3.0"),
		"myapp", "1.3.0", false); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	writeExecutable(t, filepath.Join(baseDir, "myapp", "1.2.0", "bin", "myapp-1.2.0-x86_64.AppImage"), "v1.2.0")
	if _, err := useVersion(binDir, baseDir, "myapp", "1.2.0", false); err != nil {
		t.Fatalf("use failed: %v", err)
	}

	report, err := linkApp(binDir, baseDir, "myapp", false, true)
	if err != nil {
		t.Fatalf("link failed: %v", err)
	}
	if report.Repaired != "myapp-1.2.0-x86_64.AppImage" {
		t.Errorf("repaired %q, want the legacy filename", report.Repaired)
	}
	if len(report.Errs) != 0 {
		t.Errorf("unexpected errors: %v", report.Errs)
	}

	assertLinkResolves(t, filepath.Join(binDir, "myapp"),
		filepath.Join(baseDir, "myapp", "1.2.0", "bin", "myapp"))
	if got := dirEntries(t, binDir); !slices.Equal(got, []string{"myapp"}) {
		t.Errorf("bin dir contains %v, want [myapp]: broken links should be cleaned up", got)
	}

	// Switching back to the version installed by the current lav still works.
	if _, err := useVersion(binDir, baseDir, "myapp", "1.3.0", false); err != nil {
		t.Fatalf("use failed: %v", err)
	}
	assertLinkResolves(t, filepath.Join(binDir, "myapp"),
		filepath.Join(baseDir, "myapp", "1.3.0", "bin", "myapp"))
}

func TestLinkApp_LeavesForeignEntriesAlone(t *testing.T) {
	binDir, baseDir := newDirs(t)
	if err := installBinary(binDir, baseDir, sourceBinary(t, "myapp", "v1.0.0"),
		"myapp", "1.0.0", false); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	if err := os.Remove(filepath.Join(binDir, "myapp")); err != nil {
		t.Fatalf("failed to remove link: %v", err)
	}

	elsewhere := sourceBinary(t, "other", "not managed by lav")
	if err := os.Symlink(elsewhere, filepath.Join(binDir, "myapp")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	report, err := linkApp(binDir, baseDir, "myapp", false, true)
	if err != nil {
		t.Fatalf("link failed: %v", err)
	}
	if len(report.Errs) == 0 {
		t.Error("expected the foreign symlink to be reported")
	}
	if target, _ := os.Readlink(filepath.Join(binDir, "myapp")); target != elsewhere {
		t.Errorf("the user's symlink was replaced without --force (now %s)", target)
	}

	report, err = linkApp(binDir, baseDir, "myapp", true, true)
	if err != nil {
		t.Fatalf("link --force failed: %v", err)
	}
	if len(report.Errs) != 0 {
		t.Errorf("unexpected errors: %v", report.Errs)
	}
	assertLinkResolves(t, filepath.Join(binDir, "myapp"),
		filepath.Join(baseDir, "myapp", "1.0.0", "bin", "myapp"))
}

func TestLinkApp_NoCurrentVersion(t *testing.T) {
	binDir, baseDir := newDirs(t)
	if err := os.MkdirAll(filepath.Join(baseDir, "myapp", "1.0.0", "bin"), 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	if _, err := linkApp(binDir, baseDir, "myapp", false, true); err == nil {
		t.Error("expected an error when no version is current")
	}
}

// --- link targets and name validation ---

func TestBinLinkTarget_DefaultLayoutStaysRelative(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "user")
	got := binLinkTarget(
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".local", "share", "lav"),
		"myapp", "myapp")

	want := filepath.Join("..", "share", "lav", "myapp", currentName, "bin", "myapp")
	if got != want {
		t.Errorf("target is %s, want %s", got, want)
	}
}

func TestBinLinkTarget_CustomRootResolves(t *testing.T) {
	binDir := filepath.Join(string(filepath.Separator), "home", "user", ".local", "bin")
	baseDir := filepath.Join(string(filepath.Separator), "opt", "lav")

	got := binLinkTarget(binDir, baseDir, "myapp", "myapp")
	resolved := filepath.Clean(filepath.Join(binDir, got))
	want := filepath.Join(baseDir, "myapp", currentName, "bin", "myapp")
	if resolved != want {
		t.Errorf("target %s resolves to %s, want %s", got, resolved, want)
	}
}

func TestInstallBinary_LinkResolvesOutsideTheDefaultLayout(t *testing.T) {
	// LAV_ROOT can put the tree anywhere; the link still has to resolve.
	root := t.TempDir()
	binDir := filepath.Join(root, "user", "bin")
	baseDir := filepath.Join(root, "opt", "lav-data")
	for _, dir := range []string{binDir, baseDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("failed to create %s: %v", dir, err)
		}
	}

	if err := installBinary(binDir, baseDir, sourceBinary(t, "myapp", "v1.0.0"),
		"myapp", "1.0.0", false); err != nil {
		t.Fatalf("install failed: %v", err)
	}
	assertLinkResolves(t, filepath.Join(binDir, "myapp"),
		filepath.Join(baseDir, "myapp", "1.0.0", "bin", "myapp"))
}

func TestGetBaseDir_RelativeRootBecomesAbsolute(t *testing.T) {
	t.Setenv("LAV_ROOT", "lav-data")

	dir, err := getBaseDir()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("base dir is %s, want an absolute path: link targets are computed from it", dir)
	}
}

func TestValidateAppName(t *testing.T) {
	valid := []string{"myapp", "go", "my-app", "app.v2", "godot4"}
	for _, name := range valid {
		if err := validateAppName(name); err != nil {
			t.Errorf("validateAppName(%q) = %v, want nil", name, err)
		}
	}

	invalid := []string{"", ".", "..", "a/b", `a\b`, "../../.bashrc", "-f"}
	for _, name := range invalid {
		if err := validateAppName(name); err == nil {
			t.Errorf("validateAppName(%q) = nil, want an error", name)
		}
	}
}

func TestValidateVersionName(t *testing.T) {
	valid := []string{"1.2.0", "4.6.0-stable", "v1", "2026.1"}
	for _, name := range valid {
		if err := validateVersionName(name); err != nil {
			t.Errorf("validateVersionName(%q) = %v, want nil", name, err)
		}
	}

	// "current" is the name of the symlink itself: allowing it as a version
	// leaves the app in a state no lav command can repair.
	invalid := []string{"", ".", "..", currentName, "1/2"}
	for _, name := range invalid {
		if err := validateVersionName(name); err == nil {
			t.Errorf("validateVersionName(%q) = nil, want an error", name)
		}
	}
}

func TestInstallBinary_RejectsPathTraversal(t *testing.T) {
	binDir, baseDir := newDirs(t)
	outside := filepath.Join(filepath.Dir(binDir), "victim")
	writeExecutable(t, outside, "important")

	err := installBinary(binDir, baseDir, sourceBinary(t, "x", "x"),
		filepath.Join("..", "victim"), "1.0.0", true)
	if err == nil {
		t.Fatal("expected an error for an app name containing a path separator")
	}

	content, err := os.ReadFile(outside)
	if err != nil || string(content) != "important" {
		t.Errorf("a file outside the bin directory was touched: %v %q", err, content)
	}
}

// --- filesystem helpers ---

func TestCommandNames_OnlyExecutableFiles(t *testing.T) {
	dir := t.TempDir()
	writeExecutable(t, filepath.Join(dir, "foo"), "foo")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("docs"), 0644); err != nil {
		t.Fatalf("failed to write README: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}
	if err := os.Symlink("nowhere", filepath.Join(dir, "broken")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}
	if err := os.Symlink("foo", filepath.Join(dir, "alias")); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	names, err := commandNames(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(names, []string{"alias", "foo"}) {
		t.Errorf("commandNames = %v, want [alias foo]", names)
	}
}

func TestEnsureBinLink_DirectoryIsNeverRemoved(t *testing.T) {
	binDir, baseDir := newDirs(t)
	occupied := filepath.Join(binDir, "myapp")
	if err := os.MkdirAll(filepath.Join(occupied, "keep"), 0755); err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	if _, err := ensureBinLink(binDir, baseDir, "myapp", "myapp", true); err == nil {
		t.Error("expected an error: a directory must not be replaced, even with --force")
	}
	if got := dirEntries(t, occupied); !slices.Equal(got, []string{"keep"}) {
		t.Errorf("directory contents are %v, want [keep]", got)
	}
	if got := dirEntries(t, binDir); !slices.Equal(got, []string{"myapp"}) {
		t.Errorf("bin dir contains %v, want [myapp]: a temp file was left behind", got)
	}
}

func TestCopyFileMode_ReplacesInPlace(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "myapp")
	if err := os.WriteFile(dst, []byte("old"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	src := sourceBinary(t, "source", "new")
	if err := copyFileMode(src, dst, 0755); err != nil {
		t.Fatalf("copy failed: %v", err)
	}

	content, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(content) != "new" {
		t.Errorf("content is %q, want new", content)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Errorf("mode is %v, want the executable bit set", info.Mode().Perm())
	}
	if got := dirEntries(t, dir); !slices.Equal(got, []string{"myapp"}) {
		t.Errorf("directory contains %v, want [myapp]: a temp file was left behind", got)
	}
}

func TestIsUnder(t *testing.T) {
	base := filepath.Join(string(filepath.Separator), "home", "user", ".local", "share", "lav")

	inside := []string{
		base,
		filepath.Join(base, "myapp", currentName, "bin", "myapp"),
	}
	for _, path := range inside {
		if !isUnder(base, path) {
			t.Errorf("isUnder(%s, %s) = false, want true", base, path)
		}
	}

	outside := []string{
		filepath.Join(string(filepath.Separator), "home", "user", ".local", "share", "lav-other", "x"),
		filepath.Join(string(filepath.Separator), "opt", "myapp"),
		filepath.Join(string(filepath.Separator), "home", "user", ".local", "share"),
	}
	for _, path := range outside {
		if isUnder(base, path) {
			t.Errorf("isUnder(%s, %s) = true, want false", base, path)
		}
	}
}

func TestParseArgs(t *testing.T) {
	// A flag must work wherever it is typed.
	for _, args := range [][]string{
		{"./src", "myapp", "1.0.0", "--force"},
		{"--force", "./src", "myapp", "1.0.0"},
		{"./src", "-f", "myapp", "1.0.0"},
	} {
		parsed, err := parseArgs(args, true)
		if err != nil {
			t.Fatalf("parseArgs(%v) failed: %v", args, err)
		}
		if !parsed.force {
			t.Errorf("parseArgs(%v): force not set", args)
		}
		if !slices.Equal(parsed.positional, []string{"./src", "myapp", "1.0.0"}) {
			t.Errorf("parseArgs(%v): positional = %v", args, parsed.positional)
		}
	}

	parsed, err := parseArgs([]string{"myapp", "--help"}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !parsed.help {
		t.Error("--help not recognised after a positional argument")
	}

	if _, err := parseArgs([]string{"--force"}, false); err == nil {
		t.Error("expected an error for a flag the command does not take")
	}
	if _, err := parseArgs([]string{"--nope"}, true); err == nil {
		t.Error("expected an error for an unknown flag")
	}
}
