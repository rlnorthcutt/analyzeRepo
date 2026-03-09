package fetch

import (
	"path/filepath"
	"strings"
	"testing"
)

// ── ShouldIgnore ──────────────────────────────────────────────────────────────

func TestShouldIgnore_ignoredDirs(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"node_modules/lodash/index.js", true},
		{".git/config", true},
		{"__pycache__/app.pyc", true},
		{"vendor/lib/foo.go", true},
		{".venv/lib/python3.11/site.py", true},
		{"target/debug/binary", true},
		{"src/main.go", false},
		{"internal/fetch/fetch.go", false},
	}
	for _, c := range cases {
		got := ShouldIgnore(filepath.FromSlash(c.path))
		if got != c.want {
			t.Errorf("ShouldIgnore(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestShouldIgnore_ignoredExtensions(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"app.pyc", true},
		{"lib.so", true},
		{"photo.jpg", true},
		{"icon.png", true},
		{"archive.zip", true},
		{"movie.mp4", true},
		{"font.woff2", true},
		{"data.db", true},
		{"main.go", false},
		{"README.md", false},
		{"style.css", false},
	}
	for _, c := range cases {
		got := ShouldIgnore(c.path)
		if got != c.want {
			t.Errorf("ShouldIgnore(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestShouldIgnore_ignoredFilenames(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{".env", true},
		{".env.local", true},
		{"package-lock.json", true},
		{"yarn.lock", true},
		{"Cargo.lock", true},
		{".DS_Store", true},
		{"Thumbs.db", true},
		{"go.sum", false},
		{"go.mod", false},
		{"Makefile", false},
	}
	for _, c := range cases {
		got := ShouldIgnore(c.path)
		if got != c.want {
			t.Errorf("ShouldIgnore(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestShouldIgnore_nestedIgnoredDir(t *testing.T) {
	// A deeply nested ignored dir mid-path should still be caught.
	if !ShouldIgnore(filepath.FromSlash("src/node_modules/pkg/index.js")) {
		t.Error("expected nested node_modules path to be ignored")
	}
	if ShouldIgnore(filepath.FromSlash("src/mymodule/index.js")) {
		t.Error("expected src/mymodule/index.js to not be ignored")
	}
}

// ── isGitHubURL ───────────────────────────────────────────────────────────────

func TestIsGitHubURL(t *testing.T) {
	cases := []struct {
		source string
		want   bool
	}{
		{"https://github.com/user/repo", true},
		{"http://github.com/user/repo", true},
		{"git@github.com:user/repo.git", true},
		{"https://gitlab.com/user/repo", false},
		{"/local/path/to/repo", false},
		{"./relative/path", false},
		{"", false},
	}
	for _, c := range cases {
		got := isGitHubURL(c.source)
		if got != c.want {
			t.Errorf("isGitHubURL(%q) = %v, want %v", c.source, got, c.want)
		}
	}
}

// ── BuildTreeString ───────────────────────────────────────────────────────────

func TestBuildTreeString_flat(t *testing.T) {
	root := "/repo"
	files := []string{
		"/repo/main.go",
		"/repo/go.mod",
	}
	tree := BuildTreeString(root, files)

	if !strings.HasPrefix(tree, "repo/\n") {
		t.Errorf("tree should start with root dir; got:\n%s", tree)
	}
	if !strings.Contains(tree, "main.go") {
		t.Error("tree missing main.go")
	}
	if !strings.Contains(tree, "go.mod") {
		t.Error("tree missing go.mod")
	}
}

func TestBuildTreeString_nested(t *testing.T) {
	root := "/repo"
	files := []string{
		"/repo/cmd/root.go",
		"/repo/internal/fetch/fetch.go",
		"/repo/main.go",
	}
	tree := BuildTreeString(root, files)

	if !strings.Contains(tree, "cmd") {
		t.Error("tree missing cmd directory")
	}
	if !strings.Contains(tree, "internal") {
		t.Error("tree missing internal directory")
	}
	if !strings.Contains(tree, "fetch.go") {
		t.Error("tree missing fetch.go")
	}
	if !strings.Contains(tree, "main.go") {
		t.Error("tree missing main.go")
	}
}

func TestBuildTreeString_empty(t *testing.T) {
	tree := BuildTreeString("/repo", nil)
	if !strings.HasPrefix(tree, "repo/") {
		t.Errorf("empty tree should still show root; got %q", tree)
	}
}
