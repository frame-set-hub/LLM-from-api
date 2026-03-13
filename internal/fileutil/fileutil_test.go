package fileutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ResolvePath
// ---------------------------------------------------------------------------

func TestResolvePath_Absolute(t *testing.T) {
	got := ResolvePath("/tmp/foo.go", "/home/user/project")
	if got != "/tmp/foo.go" {
		t.Errorf("expected /tmp/foo.go, got %s", got)
	}
}

func TestResolvePath_Relative(t *testing.T) {
	got := ResolvePath("src/main.go", "/home/user/project")
	want := filepath.Join("/home/user/project", "src/main.go")
	if got != want {
		t.Errorf("expected %s, got %s", want, got)
	}
}

func TestResolvePath_Tilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}
	got := ResolvePath("~/Documents/file.txt", "/some/workdir")
	want := filepath.Join(home, "Documents/file.txt")
	if got != want {
		t.Errorf("expected %s, got %s", want, got)
	}
}

func TestResolvePath_DotRelative(t *testing.T) {
	got := ResolvePath("./foo.go", "/work")
	want := filepath.Join("/work", "foo.go")
	if got != want {
		t.Errorf("expected %s, got %s", want, got)
	}
}

// ---------------------------------------------------------------------------
// InjectFile
// ---------------------------------------------------------------------------

func TestInjectFile_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.go")
	if err := os.WriteFile(path, []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := InjectFile(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "[File: hello.go]") {
		t.Error("expected file header in output")
	}
	if !strings.Contains(got, "```go") {
		t.Error("expected go fence in output")
	}
	if !strings.Contains(got, "package main") {
		t.Error("expected file content in output")
	}
}

func TestInjectFile_WithQuestion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.json")
	if err := os.WriteFile(path, []byte(`{"key":"val"}`), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := InjectFile(path, "what is this?")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "what is this?") {
		t.Error("expected question appended to output")
	}
}

func TestInjectFile_NotFound(t *testing.T) {
	_, err := InjectFile("/nonexistent/file.go", "")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

// ---------------------------------------------------------------------------
// ResolveAtMentions
// ---------------------------------------------------------------------------

func TestResolveAtMentions_NoMatch(t *testing.T) {
	out, injected, err := ResolveAtMentions("hello world", "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if injected {
		t.Error("expected no injection")
	}
	if out != "hello world" {
		t.Errorf("expected unchanged input, got %s", out)
	}
}

func TestResolveAtMentions_WithFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	if err := os.WriteFile(path, []byte("package test"), 0644); err != nil {
		t.Fatal(err)
	}

	input := "look at @test.go please"
	out, injected, err := ResolveAtMentions(input, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !injected {
		t.Error("expected injection")
	}
	if !strings.Contains(out, "[File: test.go]") {
		t.Error("expected file block in output")
	}
	if !strings.Contains(out, "package test") {
		t.Error("expected file content in output")
	}
	if !strings.Contains(out, "please") {
		t.Error("expected surrounding text preserved")
	}
}

func TestResolveAtMentions_FileNotFound(t *testing.T) {
	out, injected, err := ResolveAtMentions("see @nonexistent.xyz", "/tmp")
	if err != nil {
		t.Fatal(err)
	}
	if injected {
		t.Error("expected no injection for missing file")
	}
	if !strings.Contains(out, "@nonexistent.xyz") {
		t.Error("expected @token preserved when file not found")
	}
}

func TestResolveAtMentions_TryExtensions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}

	out, injected, err := ResolveAtMentions("check @main", dir)
	if err != nil {
		t.Fatal(err)
	}
	if !injected {
		t.Error("expected injection via extension fallback")
	}
	if !strings.Contains(out, "package main") {
		t.Error("expected main.go content")
	}
}

// ---------------------------------------------------------------------------
// ResolveQuotedPaths
// ---------------------------------------------------------------------------

func TestResolveQuotedPaths_NoMatch(t *testing.T) {
	out, injected := ResolveQuotedPaths("just text", "/tmp")
	if injected {
		t.Error("expected no injection")
	}
	if out != "just text" {
		t.Error("expected unchanged input")
	}
}

func TestResolveQuotedPaths_WithAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(path, []byte("hello data"), 0644); err != nil {
		t.Fatal(err)
	}

	input := `check "` + path + `" for info`
	out, injected := ResolveQuotedPaths(input, "/tmp")
	if !injected {
		t.Error("expected injection")
	}
	if !strings.Contains(out, "hello data") {
		t.Error("expected file content in output")
	}
}

// ---------------------------------------------------------------------------
// TryExtensions
// ---------------------------------------------------------------------------

func TestTryExtensions_Found(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.py"), []byte("print('hi')"), 0644); err != nil {
		t.Fatal(err)
	}

	data, path, err := TryExtensions(filepath.Join(dir, "app"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "print('hi')" {
		t.Errorf("unexpected content: %s", data)
	}
	if !strings.HasSuffix(path, ".py") {
		t.Errorf("expected .py extension, got %s", path)
	}
}

func TestTryExtensions_NotFound(t *testing.T) {
	_, _, err := TryExtensions("/nonexistent/base")
	if err == nil {
		t.Error("expected error")
	}
}

// ---------------------------------------------------------------------------
// FormatSize
// ---------------------------------------------------------------------------

func TestFormatSize(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0B"},
		{512, "512B"},
		{1023, "1023B"},
		{1024, "1.0KB"},
		{1536, "1.5KB"},
		{1048576, "1.0MB"},
		{2621440, "2.5MB"},
	}
	for _, tc := range tests {
		got := FormatSize(tc.input)
		if got != tc.want {
			t.Errorf("FormatSize(%d) = %s, want %s", tc.input, got, tc.want)
		}
	}
}
