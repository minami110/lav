package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// version is set by ldflags during build
var version = "dev"

func getBaseDir() (string, error) {
	var dir string

	switch {
	case os.Getenv("LAV_ROOT") != "":
		// 1. LAV_ROOT environment variable (highest priority)
		dir = os.Getenv("LAV_ROOT")
	case os.Getenv("XDG_DATA_HOME") != "":
		// 2. XDG_DATA_HOME environment variable
		dir = filepath.Join(os.Getenv("XDG_DATA_HOME"), "lav")
	default:
		// 3. Fallback to ~/.local/share/lav
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "share", "lav")
	}

	// Symlink targets are computed against this path, so a relative LAV_ROOT
	// must not leak into them.
	return filepath.Abs(dir)
}

// getLocalBinDir returns the directory the app's commands are linked into.
func getLocalBinDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin"), nil
}

// validateName rejects names that would escape the lav directory or be
// mistaken for a flag. App names become filenames in the bin directory, so this
// runs before anything is created.
func validateName(kind, name string) error {
	switch {
	case name == "":
		return fmt.Errorf("%s name must not be empty", kind)
	case name == "." || name == "..":
		return fmt.Errorf("invalid %s name %q", kind, name)
	case strings.ContainsAny(name, `/\`):
		return fmt.Errorf("invalid %s name %q: must not contain a path separator", kind, name)
	case strings.HasPrefix(name, "-"):
		return fmt.Errorf("invalid %s name %q: must not start with '-'", kind, name)
	}
	return nil
}

func validateAppName(app string) error {
	return validateName("app", app)
}

func validateVersionName(v string) error {
	if err := validateName("version", v); err != nil {
		return err
	}
	if v == currentName {
		return fmt.Errorf("invalid version name %q: reserved by lav", v)
	}
	return nil
}

func listApps(baseDir string) ([]string, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, err
	}

	var apps []string
	for _, entry := range entries {
		if entry.IsDir() {
			apps = append(apps, entry.Name())
		}
	}

	sort.Strings(apps)
	return apps, nil
}

func listVersions(baseDir, app string) ([]string, error) {
	appDir := filepath.Join(baseDir, app)
	entries, err := os.ReadDir(appDir)
	if err != nil {
		return nil, err
	}

	var versions []string
	for _, entry := range entries {
		if entry.IsDir() && entry.Name() != currentName {
			versions = append(versions, entry.Name())
		}
	}

	sort.Strings(versions)
	return versions, nil
}

func getCurrentVersion(baseDir, app string) (string, error) {
	currentLink := filepath.Join(baseDir, app, currentName)
	target, err := os.Readlink(currentLink)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}

	return filepath.Base(target), nil
}

// checkCurrentReplaceable reports whether the current symlink can be repointed,
// without changing anything.
func checkCurrentReplaceable(appDir string) error {
	currentLink := filepath.Join(appDir, currentName)

	info, err := os.Lstat(currentLink)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to inspect %s: %w", currentLink, err)
	}

	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s exists and is not a symlink; remove it (rm -rf %s) and re-run", currentLink, currentLink)
	}
	return nil
}

// repointCurrent points <app>/current at version.
func repointCurrent(appDir, version string) error {
	if err := checkCurrentReplaceable(appDir); err != nil {
		return err
	}
	return replaceSymlink(filepath.Join(appDir, currentName), version)
}

func switchVersion(baseDir, app, version string) error {
	if err := validateAppName(app); err != nil {
		return err
	}
	if err := validateVersionName(version); err != nil {
		return err
	}

	appDir := filepath.Join(baseDir, app)
	versionDir := filepath.Join(appDir, version)

	info, err := os.Stat(versionDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("version %s does not exist for %s", version, app)
		}
		return fmt.Errorf("failed to read %s: %w", versionDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", versionDir)
	}

	return repointCurrent(appDir, version)
}

func installBinary(binDir, baseDir, binaryPath, appName, version string, force bool) error {
	if err := validateAppName(appName); err != nil {
		return err
	}
	if err := validateVersionName(version); err != nil {
		return err
	}

	// Get absolute path of the binary
	absPath, err := filepath.Abs(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", absPath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", absPath)
	}

	appDir := filepath.Join(baseDir, appName)

	// Everything that can fail is checked before any state changes: an install
	// must never take effect and report failure at the same time.
	if err := checkBinLinks(binDir, baseDir, appName, []string{appName}, force); err != nil {
		return err
	}
	if err := checkCurrentReplaceable(appDir); err != nil {
		return err
	}

	// Store the binary under the app name rather than the source filename, so
	// that <version>/bin/<app> and <binDir>/<app> stay stable across versions
	// even when the release asset carries the version in its name.
	versionBinDir := filepath.Join(appDir, version, "bin")
	if err := os.MkdirAll(versionBinDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	if err := copyFileMode(absPath, filepath.Join(versionBinDir, appName), 0755); err != nil {
		return fmt.Errorf("failed to copy binary: %w", err)
	}

	return finishInstall(binDir, baseDir, appName, version, []string{appName}, force)
}

// finishInstall links the app's commands and only then makes the new version
// current.
//
// Linking first means a failure leaves the app running the version it was on,
// so the exit status matches the actual state. On a first install there is no
// such version, and the links just created would point at a current symlink
// that was never made, so they are taken back out.
func finishInstall(binDir, baseDir, appName, version string, names []string, force bool) error {
	previous, err := getCurrentVersion(baseDir, appName)
	if err != nil {
		return err
	}

	rollback := func(linked []string) {
		if previous != "" {
			return
		}
		for _, name := range linked {
			os.Remove(filepath.Join(binDir, name))
		}
	}

	report := linkBinNames(binDir, baseDir, appName, names, force)
	if len(report.Errs) > 0 {
		rollback(report.Linked)
		return errors.Join(report.Errs...)
	}

	if err := repointCurrent(filepath.Join(baseDir, appName), version); err != nil {
		rollback(report.Linked)
		return err
	}
	return nil
}

// copyFileMode copies src to dst with the given permissions.
//
// The copy is written to a temporary file and renamed into place, so a failure
// halfway through cannot truncate a working binary, and replacing a binary that
// is currently running does not fail with ETXTBSY.
func copyFileMode(src, dst string, mode os.FileMode) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	tmpFile, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".lav-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer func() {
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()

	if _, err := io.Copy(tmpFile, sourceFile); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Chmod(mode); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpName, dst); err != nil {
		return err
	}
	tmpName = ""
	return nil
}

func copyDir(src, dst string) error {
	// Get source directory info
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	// Create destination directory
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	// Read all entries in source directory
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			// Recursively copy subdirectory
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}

		// Copy file, preserving its permissions
		srcFileInfo, err := os.Stat(srcPath)
		if err != nil {
			return err
		}
		if err := copyFileMode(srcPath, dstPath, srcFileInfo.Mode().Perm()); err != nil {
			return err
		}
	}

	return nil
}

func installDirectory(binDir, baseDir, srcDir, appName, version string, force bool) error {
	if err := validateAppName(appName); err != nil {
		return err
	}
	if err := validateVersionName(version); err != nil {
		return err
	}

	// Get absolute path of the source directory
	absPath, err := filepath.Abs(srcDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	srcInfo, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("failed to read source directory %s: %w", absPath, err)
	}
	if !srcInfo.IsDir() {
		return fmt.Errorf("source path is not a directory: %s", absPath)
	}

	// Check if bin/ directory exists in source
	srcBinDir := filepath.Join(absPath, "bin")
	binInfo, err := os.Stat(srcBinDir)
	if err != nil {
		return fmt.Errorf("bin/ directory not found in %s: %w", absPath, err)
	}
	if !binInfo.IsDir() {
		return fmt.Errorf("%s is not a directory", srcBinDir)
	}

	// The commands to link are known from the source, so conflicts can be
	// reported before anything is copied.
	names, err := commandNames(srcBinDir)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", srcBinDir, err)
	}
	if len(names) == 0 {
		return fmt.Errorf("no executable files found in %s", srcBinDir)
	}

	appDir := filepath.Join(baseDir, appName)
	if err := checkBinLinks(binDir, baseDir, appName, names, force); err != nil {
		return err
	}
	if err := checkCurrentReplaceable(appDir); err != nil {
		return err
	}

	// Copy entire directory structure
	versionDir := filepath.Join(appDir, version)
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		return fmt.Errorf("failed to create version directory: %w", err)
	}
	if err := copyDir(absPath, versionDir); err != nil {
		return fmt.Errorf("failed to copy directory: %w", err)
	}

	return finishInstall(binDir, baseDir, appName, version, names, force)
}

// cmdArgs is the result of splitting a subcommand's arguments into flags and
// positional arguments. Flags are accepted in any position.
type cmdArgs struct {
	positional []string
	force      bool
	help       bool
}

func parseArgs(args []string, allowForce bool) (cmdArgs, error) {
	var parsed cmdArgs

	for _, arg := range args {
		switch arg {
		case "--help", "-h":
			parsed.help = true
		case "--force", "-f":
			if !allowForce {
				return parsed, fmt.Errorf("unknown flag: %s", arg)
			}
			parsed.force = true
		default:
			if len(arg) > 1 && strings.HasPrefix(arg, "-") {
				return parsed, fmt.Errorf("unknown flag: %s", arg)
			}
			parsed.positional = append(parsed.positional, arg)
		}
	}

	return parsed, nil
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  lav install <path> <app> <version>  Install a binary or folder")
	fmt.Println("  lav use <app> [version]             Switch to a specific version")
	fmt.Println("  lav link [app]                      Recreate missing command symlinks")
	fmt.Println("  lav list [app]                      List all apps or versions for a specific app")
	fmt.Println("  lav current [app]                   Show current version for an app or all apps")
	fmt.Println("  lav --version, -v                   Show version information")
	fmt.Println("  lav --help, -h, help                Show this help message")
	fmt.Println()
	fmt.Println("Use 'lav <command> --help' for more information about a command.")
}

func printInstallHelp() {
	fmt.Println("Usage: lav install <path> <app> <version> [--force]")
	fmt.Println()
	fmt.Println("Install a binary or folder to the apps structure.")
	fmt.Println("A single binary is stored as <app>/<version>/bin/<app>, regardless of")
	fmt.Println("the source filename, and linked into ~/.local/bin/<app>.")
	fmt.Println()
	fmt.Println("Arguments:")
	fmt.Println("  <path>     Path to a binary file or folder containing bin/")
	fmt.Println("  <app>      Application name")
	fmt.Println("  <version>  Version string (e.g., 1.0.0)")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --force, -f  Replace an existing ~/.local/bin entry that lav does not manage")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  lav install ./lav lav 0.0.0")
	fmt.Println("  lav install ~/Downloads/myapp-1.2.0-x86_64.AppImage myapp 1.2.0")
	fmt.Println("  lav install ~/Downloads/go1.25.6.linux-amd64/go go 1.25.6")
}

func printUseHelp() {
	fmt.Println("Usage: lav use <app> [version] [--force]")
	fmt.Println()
	fmt.Println("Switch to a specific version of an installed application.")
	fmt.Println("If version is omitted, shows an interactive version selector.")
	fmt.Println("Missing command symlinks in ~/.local/bin are recreated as well.")
	fmt.Println()
	fmt.Println("Arguments:")
	fmt.Println("  <app>      Application name")
	fmt.Println("  [version]  Version to switch to (optional)")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --force, -f  Replace an existing ~/.local/bin entry that lav does not manage")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  lav use go 1.25.6    # Switch to specific version")
	fmt.Println("  lav use go           # Interactive version selection")
}

func printLinkHelp() {
	fmt.Println("Usage: lav link [app] [--force]")
	fmt.Println()
	fmt.Println("Recreate the ~/.local/bin symlinks for the current version of an")
	fmt.Println("application, and remove the ones lav left behind that no longer resolve.")
	fmt.Println("If app is omitted, every installed application is processed.")
	fmt.Println()
	fmt.Println("An app installed by an older lav, which stored the binary under its")
	fmt.Println("source filename, is repaired by renaming that file to bin/<app>. This")
	fmt.Println("only happens when ~/.local/bin/<app> is a lav symlink that no longer")
	fmt.Println("resolves, so a package whose command is named differently from the app")
	fmt.Println("is left as it is.")
	fmt.Println()
	fmt.Println("Arguments:")
	fmt.Println("  [app]  Optional application name")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  --force, -f  Replace an existing ~/.local/bin entry that lav does not manage")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  lav link myapp           # Repair the links of one app")
	fmt.Println("  lav link myapp --force   # Replace a hand-installed ~/.local/bin/myapp")
	fmt.Println("  lav link                 # Repair the links of every app")
}

func printListHelp() {
	fmt.Println("Usage: lav list [app]")
	fmt.Println()
	fmt.Println("List all installed applications or versions for a specific application.")
	fmt.Println()
	fmt.Println("Arguments:")
	fmt.Println("  [app]  Optional application name to list versions for")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  lav list         # List all applications")
	fmt.Println("  lav list go      # List versions of go")
}

func printCurrentHelp() {
	fmt.Println("Usage: lav current [app]")
	fmt.Println()
	fmt.Println("Show the current version for an application or all applications.")
	fmt.Println()
	fmt.Println("Arguments:")
	fmt.Println("  [app]  Optional application name")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  lav current      # Show current versions of all apps")
	fmt.Println("  lav current go   # Show current version of go")
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}

func usageError(usage string) {
	fmt.Fprintln(os.Stderr, usage)
	os.Exit(1)
}

func runInstall(baseDir string, args []string) {
	parsed, err := parseArgs(args, true)
	if err != nil {
		fatal(err)
	}
	if parsed.help {
		printInstallHelp()
		return
	}
	if len(parsed.positional) != 3 {
		usageError("Usage: lav install <path> <app> <version> [--force]")
	}

	srcPath, appName, appVersion := parsed.positional[0], parsed.positional[1], parsed.positional[2]

	binDir, err := getLocalBinDir()
	if err != nil {
		fatal(err)
	}

	srcInfo, err := os.Stat(srcPath)
	if err != nil {
		fatal(err)
	}

	if srcInfo.IsDir() {
		err = installDirectory(binDir, baseDir, srcPath, appName, appVersion, parsed.force)
	} else {
		err = installBinary(binDir, baseDir, srcPath, appName, appVersion, parsed.force)
	}
	if err != nil {
		fatal(err)
	}

	fmt.Printf("Installed %s version %s\n", appName, appVersion)

	// The install itself is complete at this point. Links left behind by an
	// earlier version are worth reporting, but they do not make it a failure.
	if dangling, err := danglingAppLinks(binDir, baseDir, appName); err == nil {
		for _, name := range dangling {
			fmt.Fprintf(os.Stderr, "warning: %s no longer resolves; run 'lav link %s' to remove it\n",
				filepath.Join(binDir, name), appName)
		}
	}
}

func runUse(baseDir string, args []string) {
	parsed, err := parseArgs(args, true)
	if err != nil {
		fatal(err)
	}
	if parsed.help {
		printUseHelp()
		return
	}
	if len(parsed.positional) < 1 || len(parsed.positional) > 2 {
		usageError("Usage: lav use <app> [version] [--force]")
	}

	app := parsed.positional[0]

	binDir, err := getLocalBinDir()
	if err != nil {
		fatal(err)
	}

	var selected string
	if len(parsed.positional) == 2 {
		selected = parsed.positional[1]
	} else {
		// インタラクティブモード
		versions, err := listVersions(baseDir, app)
		if err != nil {
			fatal(err)
		}
		if len(versions) == 0 {
			fmt.Fprintf(os.Stderr, "No versions installed for %s\n", app)
			os.Exit(1)
		}
		current, _ := getCurrentVersion(baseDir, app)

		choice, cancelled, err := selectVersionInteractive(app, versions, current)
		if err != nil {
			fatal(err)
		}
		if cancelled {
			return
		}
		selected = choice
	}

	report, err := useVersion(binDir, baseDir, app, selected, parsed.force)
	if err != nil {
		fatal(err)
	}

	fmt.Printf("Switched %s to version %s\n", app, selected)

	// The version switch succeeded, but reporting success while the command is
	// not actually reachable is what makes a broken link invisible.
	for _, warning := range report.Warnings(app) {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}
	if report.Failed() {
		fmt.Fprintf(os.Stderr, "%s is not fully reachable from %s\n", app, binDir)
		os.Exit(1)
	}
}

func runLink(baseDir string, args []string) {
	parsed, err := parseArgs(args, true)
	if err != nil {
		fatal(err)
	}
	if parsed.help {
		printLinkHelp()
		return
	}
	if len(parsed.positional) > 1 {
		usageError("Usage: lav link [app] [--force]")
	}

	binDir, err := getLocalBinDir()
	if err != nil {
		fatal(err)
	}

	var apps []string
	if len(parsed.positional) == 1 {
		apps = []string{parsed.positional[0]}
	} else {
		apps, err = listApps(baseDir)
		if err != nil {
			fatal(err)
		}
	}

	failed := false
	for _, app := range apps {
		report, err := linkApp(binDir, baseDir, app, parsed.force, true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s: %v\n", app, err)
			failed = true
			continue
		}
		if printLinkReport(app, binDir, report) {
			failed = true
		}
	}

	if failed {
		os.Exit(1)
	}
}

// printLinkReport prints what lav link did for one app and reports whether
// anything failed.
func printLinkReport(app, binDir string, report appLinkReport) bool {
	if report.Repaired != "" {
		fmt.Printf("%s: renamed bin/%s to bin/%s\n", app, report.Repaired, app)
	}
	for _, name := range report.Linked {
		fmt.Printf("%s: linked %s\n", app, filepath.Join(binDir, name))
	}
	for _, name := range report.Pruned {
		fmt.Printf("%s: removed broken symlink %s\n", app, filepath.Join(binDir, name))
	}
	for _, err := range report.Errs {
		fmt.Fprintf(os.Stderr, "Error: %s: %v\n", app, err)
	}

	if report.Repaired == "" && len(report.Linked) == 0 && len(report.Pruned) == 0 && len(report.Errs) == 0 {
		fmt.Printf("%s: already linked (%d command(s))\n", app, len(report.Unchanged))
	}

	return len(report.Errs) > 0
}

func runList(baseDir string, args []string) {
	parsed, err := parseArgs(args, false)
	if err != nil {
		fatal(err)
	}
	if parsed.help {
		printListHelp()
		return
	}

	switch len(parsed.positional) {
	case 0:
		apps, err := listApps(baseDir)
		if err != nil {
			fatal(err)
		}
		for _, app := range apps {
			current, _ := getCurrentVersion(baseDir, app)
			if current != "" {
				fmt.Printf("%s (current: %s)\n", app, current)
			} else {
				fmt.Println(app)
			}
		}
	case 1:
		app := parsed.positional[0]
		if err := validateAppName(app); err != nil {
			fatal(err)
		}
		versions, err := listVersions(baseDir, app)
		if err != nil {
			fatal(err)
		}
		current, _ := getCurrentVersion(baseDir, app)
		for _, v := range versions {
			if v == current {
				fmt.Printf("%s (current)\n", v)
			} else {
				fmt.Println(v)
			}
		}
	default:
		usageError("Usage: lav list [app]")
	}
}

func runCurrent(baseDir string, args []string) {
	parsed, err := parseArgs(args, false)
	if err != nil {
		fatal(err)
	}
	if parsed.help {
		printCurrentHelp()
		return
	}

	switch len(parsed.positional) {
	case 0:
		apps, err := listApps(baseDir)
		if err != nil {
			fatal(err)
		}
		for _, app := range apps {
			current, _ := getCurrentVersion(baseDir, app)
			if current != "" {
				fmt.Printf("%s: %s\n", app, current)
			}
		}
	case 1:
		app := parsed.positional[0]
		if err := validateAppName(app); err != nil {
			fatal(err)
		}
		current, err := getCurrentVersion(baseDir, app)
		if err != nil {
			fatal(err)
		}
		if current == "" {
			fmt.Fprintf(os.Stderr, "No current version set for %s\n", app)
			os.Exit(1)
		}
		fmt.Println(current)
	default:
		usageError("Usage: lav current [app]")
	}
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "--help", "-h", "help":
		printUsage()
		return
	case "--version", "-v":
		fmt.Printf("lav %s\n", version)
		return
	}

	baseDir, err := getBaseDir()
	if err != nil {
		fatal(err)
	}

	args := os.Args[2:]

	switch command {
	case "install":
		runInstall(baseDir, args)
	case "use":
		runUse(baseDir, args)
	case "link":
		runLink(baseDir, args)
	case "list":
		runList(baseDir, args)
	case "current":
		runCurrent(baseDir, args)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}
