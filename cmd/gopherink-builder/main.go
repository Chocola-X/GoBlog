package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const generatedPluginFile = "zz_plugins_autoload.go"

type discoveredPlugin struct {
	Directory  string
	ImportPath string
	ModuleDir  string
}

func main() {
	var (
		output      string
		tags        string
		listOnly    bool
		verbose     bool
		trimPath    bool
		raceEnabled bool
	)
	flag.StringVar(&output, "o", "gopherink", "output binary path")
	flag.StringVar(&tags, "tags", "", "comma-separated Go build tags")
	flag.BoolVar(&listOnly, "list", false, "list discovered plugins without building")
	flag.BoolVar(&verbose, "v", false, "print packages while building")
	flag.BoolVar(&trimPath, "trimpath", false, "remove local file system paths from the binary")
	flag.BoolVar(&raceEnabled, "race", false, "enable the Go race detector")
	flag.Parse()

	if err := run(output, tags, listOnly, verbose, trimPath, raceEnabled); err != nil {
		fmt.Fprintln(os.Stderr, "gopherink-builder:", err)
		os.Exit(1)
	}
}

func run(output, tags string, listOnly, verbose, trimPath, raceEnabled bool) error {
	root, err := projectRoot()
	if err != nil {
		return err
	}
	rootModule, err := modulePath(root)
	if err != nil {
		return fmt.Errorf("read root module: %w", err)
	}
	plugins, err := discoverPlugins(root, rootModule)
	if err != nil {
		return err
	}
	for _, item := range plugins {
		fmt.Printf("plugin: %s (%s)\n", item.Directory, item.ImportPath)
	}
	if listOnly {
		return nil
	}

	buildDir := filepath.Join(root, ".gopherink-build")
	if err := os.RemoveAll(buildDir); err != nil {
		return fmt.Errorf("clean temporary build directory: %w", err)
	}
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return fmt.Errorf("create temporary build directory: %w", err)
	}
	defer os.RemoveAll(buildDir)

	generatedPath := filepath.Join(root, "cmd", "gopherink", generatedPluginFile)
	if err := os.Remove(generatedPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale generated plugin imports: %w", err)
	}
	defer os.Remove(generatedPath)
	if err := writePluginImports(generatedPath, plugins); err != nil {
		return err
	}

	workspacePath := filepath.Join(buildDir, "go.work")
	if err := writeWorkspace(workspacePath, root, plugins); err != nil {
		return err
	}

	if !filepath.IsAbs(output) {
		output = filepath.Join(root, output)
	}
	args := []string{"build", "-o", output}
	if strings.TrimSpace(tags) != "" {
		args = append(args, "-tags", tags)
	}
	if verbose {
		args = append(args, "-v")
	}
	if trimPath {
		args = append(args, "-trimpath")
	}
	if raceEnabled {
		args = append(args, "-race")
	}
	args = append(args, "./cmd/gopherink")

	cmd := exec.Command("go", args...)
	cmd.Dir = root
	cmd.Env = replaceEnv(os.Environ(), "GOWORK", workspacePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build failed: %w", err)
	}
	fmt.Printf("built: %s\n", output)
	return nil
}

func projectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if info, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil && !info.IsDir() {
			if info, pluginErr := os.Stat(filepath.Join(dir, "core", "plugin")); pluginErr == nil && info.IsDir() {
				return dir, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("GopherInk project root not found")
		}
		dir = parent
	}
}

func discoverPlugins(root, rootModule string) ([]discoveredPlugin, error) {
	pluginsDir := filepath.Join(root, "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return nil, fmt.Errorf("read plugins directory: %w", err)
	}
	plugins := make([]discoveredPlugin, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}
		dir := filepath.Join(pluginsDir, name)
		hasSource, err := hasRootGoSource(dir)
		if err != nil {
			return nil, fmt.Errorf("inspect plugin %s: %w", name, err)
		}
		if !hasSource {
			continue
		}

		item := discoveredPlugin{Directory: name}
		if info, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil && !info.IsDir() {
			item.ImportPath, err = modulePath(dir)
			if err != nil {
				return nil, fmt.Errorf("read plugin module %s: %w", name, err)
			}
			item.ModuleDir = dir
		} else if statErr != nil && !os.IsNotExist(statErr) {
			return nil, fmt.Errorf("inspect plugin module %s: %w", name, statErr)
		} else {
			item.ImportPath = strings.TrimRight(rootModule, "/") + "/plugins/" + name
		}
		plugins = append(plugins, item)
	}
	sort.Slice(plugins, func(i, j int) bool {
		return plugins[i].ImportPath < plugins[j].ImportPath
	})
	return plugins, nil
}

func hasRootGoSource(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type().IsRegular() && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			return true, nil
		}
	}
	return false, nil
}

func modulePath(dir string) (string, error) {
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Path}}")
	cmd.Dir = dir
	cmd.Env = replaceEnv(os.Environ(), "GOWORK", "off")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if path == "" || path == "command-line-arguments" {
		return "", fmt.Errorf("module path is empty")
	}
	return path, nil
}

func writePluginImports(path string, plugins []discoveredPlugin) error {
	var source bytes.Buffer
	source.WriteString("// Code generated by gopherink-builder; DO NOT EDIT.\n\n")
	source.WriteString("package main\n\n")
	if len(plugins) > 0 {
		source.WriteString("import (\n")
		for _, item := range plugins {
			source.WriteString("\t_ ")
			source.WriteString(strconv.Quote(item.ImportPath))
			source.WriteByte('\n')
		}
		source.WriteString(")\n")
	}
	formatted, err := format.Source(source.Bytes())
	if err != nil {
		return fmt.Errorf("format generated plugin imports: %w", err)
	}
	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		return fmt.Errorf("write generated plugin imports: %w", err)
	}
	return nil
}

func writeWorkspace(path, root string, plugins []discoveredPlugin) error {
	version, err := goVersion(root)
	if err != nil {
		return err
	}
	workspaceDir := filepath.Dir(path)
	modules := []string{root}
	seen := map[string]bool{filepath.Clean(root): true}
	for _, item := range plugins {
		if item.ModuleDir == "" {
			continue
		}
		clean := filepath.Clean(item.ModuleDir)
		if !seen[clean] {
			seen[clean] = true
			modules = append(modules, clean)
		}
	}
	sort.Strings(modules)

	var work strings.Builder
	work.WriteString("go ")
	work.WriteString(version)
	work.WriteString("\n\nuse (\n")
	for _, module := range modules {
		rel, err := filepath.Rel(workspaceDir, module)
		if err != nil {
			return fmt.Errorf("resolve workspace module %s: %w", module, err)
		}
		work.WriteString("\t")
		work.WriteString(strconv.Quote(filepath.ToSlash(rel)))
		work.WriteByte('\n')
	}
	work.WriteString(")\n")
	if err := os.WriteFile(path, []byte(work.String()), 0o644); err != nil {
		return fmt.Errorf("write temporary workspace: %w", err)
	}
	return nil
}

func goVersion(root string) (string, error) {
	cmd := exec.Command("go", "env", "GOVERSION")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("read Go version: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("Go version is empty")
	}
	version := strings.TrimPrefix(fields[0], "go")
	if version == "" {
		return "", fmt.Errorf("Go version is empty")
	}
	return version, nil
}

func replaceEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			if !replaced {
				out = append(out, prefix+value)
				replaced = true
			}
			continue
		}
		out = append(out, item)
	}
	if !replaced {
		out = append(out, prefix+value)
	}
	return out
}
