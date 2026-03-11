// Package fetch handles repository acquisition and file system traversal.
// All functions are pure: they receive explicit inputs and return results
// without side effects beyond filesystem access.
package fetch

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// ignoreDirs contains directory names that should be skipped entirely during traversal.
var ignoreDirs = map[string]bool{
	".git": true, "__pycache__": true, "node_modules": true,
	".venv": true, "venv": true, "env": true,
	"dist": true, "build": true, ".eggs": true,
	".pytest_cache": true, ".mypy_cache": true, ".ruff_cache": true,
	"htmlcov": true, "coverage": true, ".tox": true, ".nox": true,
	".idea": true, ".vscode": true, "vendor": true, ".next": true,
	"target": true,
}

// ignoreExtensions contains file extensions to exclude from analysis.
var ignoreExtensions = map[string]bool{
	".pyc": true, ".pyo": true, ".pyd": true, ".so": true,
	".dll": true, ".dylib": true,
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true,
	".bmp": true, ".ico": true, ".webp": true,
	".pdf": true, ".zip": true, ".tar": true,
	".gz": true, ".bz2": true, ".xz": true, ".7z": true,
	".mp3": true, ".mp4": true, ".avi": true, ".mov": true,
	".woff": true, ".woff2": true, ".ttf": true, ".eot": true,
	".svg": true, ".db": true, ".sqlite": true, ".bin": true, ".dat": true,
}

// ignoreFilenames contains specific filenames to exclude regardless of extension.
var ignoreFilenames = map[string]bool{
	".DS_Store": true, "Thumbs.db": true,
	".env": true, ".env.local": true, ".env.production": true,
	"package-lock.json": true, "yarn.lock": true,
	"pnpm-lock.yaml": true, "uv.lock": true,
	"poetry.lock": true, "Cargo.lock": true,
	"composer.lock": true, "Gemfile.lock": true,
}

// ShouldIgnore reports whether the given relative path should be excluded from analysis.
// It checks all path components against ignoreDirs, the filename against ignoreFilenames,
// and the extension against ignoreExtensions.
func ShouldIgnore(relPath string) bool {
	parts := strings.SplitSeq(filepath.ToSlash(relPath), "/")
	for part := range parts {
		if ignoreDirs[part] {
			return true
		}
	}
	base := filepath.Base(relPath)
	if ignoreFilenames[base] {
		return true
	}
	return ignoreExtensions[strings.ToLower(filepath.Ext(base))]
}

// IsBinary reports whether the file at path appears to be binary by scanning
// the first 8 KB for null bytes.
func IsBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	buf := make([]byte, 8192)
	n, err := f.Read(buf)
	if err != nil {
		return true
	}
	return slices.Contains(buf[:n], 0)
}

// BuildFileList walks root and returns absolute paths of all non-ignored,
// non-binary files, sorted alphabetically.
func BuildFileList(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries silently
		}
		if d.IsDir() {
			if ignoreDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if !ShouldIgnore(rel) && !IsBinary(path) {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

// BuildTreeString renders a visual directory tree from a list of absolute paths.
// The output mimics the unix `tree` command using box-drawing characters.
// root is the repository root; files must be absolute paths under root.
func BuildTreeString(root string, files []string) string {
	// Build a nested map: directory nodes are map[string]any, leaf files are nil.
	tree := make(map[string]any)
	for _, abs := range files {
		rel, _ := filepath.Rel(root, abs)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		cur := tree
		for _, part := range parts[:len(parts)-1] {
			if _, ok := cur[part]; !ok {
				cur[part] = make(map[string]any)
			}
			cur = cur[part].(map[string]any)
		}
		cur[parts[len(parts)-1]] = nil // leaf file
	}

	var sb strings.Builder
	sb.WriteString(filepath.Base(root) + "/\n")
	renderTree(tree, &sb, "")
	return strings.TrimRight(sb.String(), "\n")
}

// renderTree recursively writes the tree structure into sb with the given prefix.
func renderTree(tree map[string]any, sb *strings.Builder, prefix string) {
	entries := make([]string, 0, len(tree))
	for name := range tree {
		entries = append(entries, name)
	}
	sort.Strings(entries)

	for i, name := range entries {
		isLast := i == len(entries)-1
		connector, extension := "├── ", "│   "
		if isLast {
			connector, extension = "└── ", "    "
		}
		child := tree[name]
		if child == nil {
			sb.WriteString(prefix + connector + name + "\n")
		} else {
			sb.WriteString(prefix + connector + name + "/\n")
			renderTree(child.(map[string]any), sb, prefix+extension)
		}
	}
}

// isGitHubURL reports whether source looks like a GitHub repository URL.
func isGitHubURL(source string) bool {
	return strings.HasPrefix(source, "https://github.com/") ||
		strings.HasPrefix(source, "http://github.com/") ||
		strings.HasPrefix(source, "git@github.com:")
}

// GetRepo resolves source to a usable local directory.
// For GitHub URLs it performs a shallow clone into a temp directory.
// Returns the repo path, a cleanup function (removes any temp dir), and any error.
func GetRepo(ctx context.Context, source string) (repoPath string, cleanup func(), err error) {
	if isGitHubURL(source) {
		tmpDir, err := os.MkdirTemp("", "repo-analyze-*")
		if err != nil {
			return "", nil, fmt.Errorf("creating temp directory: %w", err)
		}
		clonePath := filepath.Join(tmpDir, "repo")
		cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", source, clonePath)
		if out, err := cmd.CombinedOutput(); err != nil {
			os.RemoveAll(tmpDir)
			return "", nil, fmt.Errorf("git clone failed: %s", strings.TrimSpace(string(out)))
		}
		return clonePath, func() { os.RemoveAll(tmpDir) }, nil
	}

	abs, err := filepath.Abs(source)
	if err != nil {
		return "", nil, fmt.Errorf("resolving path %q: %w", source, err)
	}
	info, err := os.Stat(abs)
	if os.IsNotExist(err) {
		return "", nil, fmt.Errorf("path does not exist: %s", source)
	}
	if err != nil {
		return "", nil, fmt.Errorf("accessing path: %w", err)
	}
	if !info.IsDir() {
		return "", nil, fmt.Errorf("not a directory: %s", source)
	}
	return abs, func() {}, nil
}
