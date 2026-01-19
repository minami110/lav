package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// version is set by ldflags during build
var version = "dev"

func getBaseDir() (string, error) {
	// 1. LAV_ROOT environment variable (highest priority)
	if lavRoot := os.Getenv("LAV_ROOT"); lavRoot != "" {
		return lavRoot, nil
	}

	// 2. XDG_DATA_HOME environment variable
	if xdgDataHome := os.Getenv("XDG_DATA_HOME"); xdgDataHome != "" {
		return filepath.Join(xdgDataHome, "lav"), nil
	}

	// 3. Fallback to ~/.local/share/lav
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".local", "share", "lav"), nil
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
		if entry.IsDir() && entry.Name() != "current" {
			versions = append(versions, entry.Name())
		}
	}

	sort.Strings(versions)
	return versions, nil
}

func getCurrentVersion(baseDir, app string) (string, error) {
	currentLink := filepath.Join(baseDir, app, "current")
	target, err := os.Readlink(currentLink)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	return filepath.Base(target), nil
}

func switchVersion(baseDir, app, version string) error {
	appDir := filepath.Join(baseDir, app)
	versionDir := filepath.Join(appDir, version)

	if _, err := os.Stat(versionDir); os.IsNotExist(err) {
		return fmt.Errorf("version %s does not exist for %s", version, app)
	}

	currentLink := filepath.Join(appDir, "current")

	if info, err := os.Lstat(currentLink); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(currentLink); err != nil {
				return err
			}
		}
	}

	if err := os.Symlink(version, currentLink); err != nil {
		return err
	}

	return nil
}

func installBinary(baseDir, binaryPath, appName, version string) error {
	// Get absolute path of the binary
	absPath, err := filepath.Abs(binaryPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Check if binary exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("binary does not exist: %s", absPath)
	}

	// Get binary name (without path)
	binaryName := filepath.Base(absPath)

	// Create version directory: ~/.local/share/apps/<app>/<version>/bin/
	versionBinDir := filepath.Join(baseDir, appName, version, "bin")
	if err := os.MkdirAll(versionBinDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Copy binary to version directory
	destPath := filepath.Join(versionBinDir, binaryName)
	if err := copyFile(absPath, destPath); err != nil {
		return fmt.Errorf("failed to copy binary: %w", err)
	}

	// Make binary executable
	if err := os.Chmod(destPath, 0755); err != nil {
		return fmt.Errorf("failed to make binary executable: %w", err)
	}

	// Create/update current symlink
	appDir := filepath.Join(baseDir, appName)
	currentLink := filepath.Join(appDir, "current")

	// Remove existing symlink if it exists
	if info, err := os.Lstat(currentLink); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(currentLink); err != nil {
				return fmt.Errorf("failed to remove existing symlink: %w", err)
			}
		}
	}

	// Create current symlink
	if err := os.Symlink(version, currentLink); err != nil {
		return fmt.Errorf("failed to create current symlink: %w", err)
	}

	// Get user's home directory for ~/.local/bin
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	// Create ~/.local/bin if it doesn't exist
	localBinDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(localBinDir, 0755); err != nil {
		return fmt.Errorf("failed to create ~/.local/bin: %w", err)
	}

	// Create/update symlink in ~/.local/bin
	binLink := filepath.Join(localBinDir, binaryName)
	relTarget := filepath.Join("..", "share", "lav", appName, "current", "bin", binaryName)

	// Remove existing symlink if it exists
	if info, err := os.Lstat(binLink); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(binLink); err != nil {
				return fmt.Errorf("failed to remove existing bin symlink: %w", err)
			}
		}
	}

	// Create bin symlink
	if err := os.Symlink(relTarget, binLink); err != nil {
		return fmt.Errorf("failed to create bin symlink: %w", err)
	}

	return nil
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

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
		} else {
			// Copy file
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}

			// Preserve file permissions
			srcFileInfo, err := os.Stat(srcPath)
			if err != nil {
				return err
			}
			if err := os.Chmod(dstPath, srcFileInfo.Mode()); err != nil {
				return err
			}
		}
	}

	return nil
}

func createBinSymlinks(baseDir, appName string) error {
	// Get user's home directory for ~/.local/bin
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	// Create ~/.local/bin if it doesn't exist
	localBinDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(localBinDir, 0755); err != nil {
		return fmt.Errorf("failed to create ~/.local/bin: %w", err)
	}

	// Path to the current/bin directory
	currentBinDir := filepath.Join(baseDir, appName, "current", "bin")

	// Read all executables in current/bin
	entries, err := os.ReadDir(currentBinDir)
	if err != nil {
		return fmt.Errorf("failed to read bin directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		binName := entry.Name()
		binLink := filepath.Join(localBinDir, binName)
		relTarget := filepath.Join("..", "share", "lav", appName, "current", "bin", binName)

		// Remove existing symlink if it exists
		if info, err := os.Lstat(binLink); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				if err := os.Remove(binLink); err != nil {
					return fmt.Errorf("failed to remove existing bin symlink: %w", err)
				}
			}
		}

		// Create bin symlink
		if err := os.Symlink(relTarget, binLink); err != nil {
			return fmt.Errorf("failed to create bin symlink for %s: %w", binName, err)
		}
	}

	return nil
}

func installDirectory(baseDir, srcDir, appName, version string) error {
	// Get absolute path of the source directory
	absPath, err := filepath.Abs(srcDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Check if directory exists
	srcInfo, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("source directory does not exist: %s", absPath)
	}

	if !srcInfo.IsDir() {
		return fmt.Errorf("source path is not a directory: %s", absPath)
	}

	// Check if bin/ directory exists in source
	srcBinDir := filepath.Join(absPath, "bin")
	if _, err := os.Stat(srcBinDir); os.IsNotExist(err) {
		return fmt.Errorf("bin/ directory does not exist in source directory")
	}

	// Create version directory: ~/.local/share/apps/<app>/<version>/
	versionDir := filepath.Join(baseDir, appName, version)
	if err := os.MkdirAll(versionDir, 0755); err != nil {
		return fmt.Errorf("failed to create version directory: %w", err)
	}

	// Copy entire directory structure
	if err := copyDir(absPath, versionDir); err != nil {
		return fmt.Errorf("failed to copy directory: %w", err)
	}

	// Create/update current symlink
	appDir := filepath.Join(baseDir, appName)
	currentLink := filepath.Join(appDir, "current")

	// Remove existing symlink if it exists
	if info, err := os.Lstat(currentLink); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(currentLink); err != nil {
				return fmt.Errorf("failed to remove existing symlink: %w", err)
			}
		}
	}

	// Create current symlink
	if err := os.Symlink(version, currentLink); err != nil {
		return fmt.Errorf("failed to create current symlink: %w", err)
	}

	// Create symlinks in ~/.local/bin for all executables in bin/
	if err := createBinSymlinks(baseDir, appName); err != nil {
		return fmt.Errorf("failed to create bin symlinks: %w", err)
	}

	return nil
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  lav install <path> <app> <version>  Install a binary or folder")
	fmt.Println("  lav use <app> <version>             Switch to a specific version")
	fmt.Println("  lav list [app]                      List all apps or versions for a specific app")
	fmt.Println("  lav current [app]                   Show current version for an app or all apps")
	fmt.Println("  lav --version, -v                   Show version information")
	fmt.Println("  lav --help, -h, help                Show this help message")
	fmt.Println()
	fmt.Println("Use 'lav <command> --help' for more information about a command.")
}

func printInstallHelp() {
	fmt.Println("Usage: lav install <path> <app> <version>")
	fmt.Println()
	fmt.Println("Install a binary or folder to the apps structure.")
	fmt.Println()
	fmt.Println("Arguments:")
	fmt.Println("  <path>     Path to a binary file or folder containing bin/")
	fmt.Println("  <app>      Application name")
	fmt.Println("  <version>  Version string (e.g., 1.0.0)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  lav install ./lav lav 0.0.0")
	fmt.Println("  lav install ~/Downloads/go1.25.6.linux-amd64/go go 1.25.6")
}

func printUseHelp() {
	fmt.Println("Usage: lav use <app> <version>")
	fmt.Println()
	fmt.Println("Switch to a specific version of an installed application.")
	fmt.Println()
	fmt.Println("Arguments:")
	fmt.Println("  <app>      Application name")
	fmt.Println("  <version>  Version to switch to")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  lav use go 1.25.6")
	fmt.Println("  lav use lav 0.0.1")
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

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	// Handle help flag
	if command == "--help" || command == "-h" || command == "help" {
		printUsage()
		return
	}

	// Handle version flag
	if command == "--version" || command == "-v" {
		fmt.Printf("lav %s\n", version)
		return
	}

	baseDir, err := getBaseDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	switch command {
	case "install":
		// Check for help flag
		if len(os.Args) > 2 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
			printInstallHelp()
			return
		}

		if len(os.Args) != 5 {
			fmt.Fprintln(os.Stderr, "Usage: lav install <path> <app> <version>")
			os.Exit(1)
		}

		srcPath := os.Args[2]
		appName := os.Args[3]
		version := os.Args[4]

		// Check if srcPath is a file or directory
		srcInfo, err := os.Stat(srcPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if srcInfo.IsDir() {
			// Install directory
			if err := installDirectory(baseDir, srcPath, appName, version); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		} else {
			// Install binary
			if err := installBinary(baseDir, srcPath, appName, version); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		}

		fmt.Printf("Installed %s version %s\n", appName, version)

	case "use":
		// Check for help flag
		if len(os.Args) > 2 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
			printUseHelp()
			return
		}

		if len(os.Args) != 4 {
			fmt.Fprintln(os.Stderr, "Usage: lav use <app> <version>")
			os.Exit(1)
		}

		app := os.Args[2]
		version := os.Args[3]

		if err := switchVersion(baseDir, app, version); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Switched %s to version %s\n", app, version)

	case "list":
		// Check for help flag
		if len(os.Args) > 2 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
			printListHelp()
			return
		}

		if len(os.Args) == 2 {
			apps, err := listApps(baseDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			for _, app := range apps {
				current, _ := getCurrentVersion(baseDir, app)
				if current != "" {
					fmt.Printf("%s (current: %s)\n", app, current)
				} else {
					fmt.Println(app)
				}
			}
		} else if len(os.Args) == 3 {
			app := os.Args[2]
			versions, err := listVersions(baseDir, app)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			current, _ := getCurrentVersion(baseDir, app)
			for _, version := range versions {
				if version == current {
					fmt.Printf("%s (current)\n", version)
				} else {
					fmt.Println(version)
				}
			}
		} else {
			fmt.Fprintln(os.Stderr, "Usage: lav list [app]")
			os.Exit(1)
		}

	case "current":
		// Check for help flag
		if len(os.Args) > 2 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
			printCurrentHelp()
			return
		}

		if len(os.Args) == 2 {
			apps, err := listApps(baseDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			for _, app := range apps {
				current, _ := getCurrentVersion(baseDir, app)
				if current != "" {
					fmt.Printf("%s: %s\n", app, current)
				}
			}
		} else if len(os.Args) == 3 {
			app := os.Args[2]
			current, err := getCurrentVersion(baseDir, app)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			if current != "" {
				fmt.Println(current)
			} else {
				fmt.Fprintf(os.Stderr, "No current version set for %s\n", app)
				os.Exit(1)
			}
		} else {
			fmt.Fprintln(os.Stderr, "Usage: lav current [app]")
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}
}
