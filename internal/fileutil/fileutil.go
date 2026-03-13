// Package fileutil provides path resolution and file content injection
// for the llm-chat REPL. It handles @mentions, quoted drag-and-drop paths,
// and directory listing.
package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// AtPattern matches @filename or @/absolute/path tokens in user input.
var AtPattern = regexp.MustCompile(`@([^\s]+)`)

// QuotedPathPattern matches 'path' or "path" where path starts with / or ~/
// This covers drag-and-drop file paths on macOS.
var QuotedPathPattern = regexp.MustCompile(`['"]((~|\/)[^'"]+)['"]`)

// ResolvePath resolves p relative to workDir if it is not absolute.
func ResolvePath(p, workDir string) string {
	if filepath.IsAbs(p) {
		return p
	}
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return filepath.Join(workDir, p)
}

// InjectFile reads a file and wraps its content in a fenced code block.
// If question is non-empty it is appended after the block.
func InjectFile(path, question string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file %q: %w", path, err)
	}
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	block := fmt.Sprintf("[File: %s]\n```%s\n%s\n```", filepath.Base(path), ext, string(data))
	if question != "" {
		return block + "\n\n" + question, nil
	}
	return block, nil
}

// ResolveAtMentions scans input for @path tokens, reads each file, and
// replaces them with fenced code blocks. Returns the resolved string,
// whether any file was injected, and any error.
func ResolveAtMentions(input, workDir string) (string, bool, error) {
	matches := AtPattern.FindAllStringSubmatchIndex(input, -1)
	if len(matches) == 0 {
		return input, false, nil
	}

	var sb strings.Builder
	prev := 0
	injected := false

	for _, m := range matches {
		fullStart, fullEnd := m[0], m[1]
		rawPath := input[m[2]:m[3]]

		path := ResolvePath(rawPath, workDir)
		data, err := os.ReadFile(path)
		if err != nil {
			data, path, err = TryExtensions(path)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "[warn] could not read %s: %v\n", rawPath, err)
			sb.WriteString(input[prev:fullEnd])
			prev = fullEnd
			continue
		}

		ext := strings.TrimPrefix(filepath.Ext(path), ".")
		block := fmt.Sprintf("[File: %s]\n```%s\n%s\n```", filepath.Base(path), ext, string(data))
		sb.WriteString(input[prev:fullStart])
		sb.WriteString(block)
		prev = fullEnd
		injected = true
	}
	sb.WriteString(input[prev:])
	return sb.String(), injected, nil
}

// ResolveQuotedPaths detects dragged-file paths like '/path/file' or "/path/file"
// and replaces them with fenced file content blocks.
func ResolveQuotedPaths(input, workDir string) (string, bool) {
	matches := QuotedPathPattern.FindAllStringSubmatchIndex(input, -1)
	if len(matches) == 0 {
		return input, false
	}

	var sb strings.Builder
	prev := 0
	injected := false

	for _, m := range matches {
		fullStart, fullEnd := m[0], m[1]
		rawPath := input[m[2]:m[3]]

		path := ResolvePath(rawPath, workDir)
		data, err := os.ReadFile(path)
		if err != nil {
			sb.WriteString(input[prev:fullEnd])
			prev = fullEnd
			continue
		}

		ext := strings.TrimPrefix(filepath.Ext(path), ".")
		block := fmt.Sprintf("[File: %s]\n```%s\n%s\n```", filepath.Base(path), ext, string(data))
		sb.WriteString(input[prev:fullStart])
		sb.WriteString(block)
		prev = fullEnd
		injected = true
	}
	sb.WriteString(input[prev:])
	return sb.String(), injected
}

// TryExtensions tries common source file extensions on a base path.
func TryExtensions(base string) ([]byte, string, error) {
	exts := []string{".go", ".py", ".js", ".ts", ".md", ".json", ".yaml", ".yml", ".sh", ".txt"}
	for _, ext := range exts {
		p := base + ext
		if data, err := os.ReadFile(p); err == nil {
			return data, p, nil
		}
	}
	return nil, base, fmt.Errorf("file not found: %s", base)
}

// PrintDir lists the contents of a directory to stdout.
func PrintDir(path string) {
	entries, err := os.ReadDir(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[error] cannot read directory: %v\n", err)
		return
	}
	fmt.Printf("\n📁 %s\n", path)
	for _, e := range entries {
		if e.IsDir() {
			fmt.Printf("  📂 %s/\n", e.Name())
		} else {
			info, _ := e.Info()
			fmt.Printf("  📄 %-40s %s\n", e.Name(), FormatSize(info.Size()))
		}
	}
}

// FormatSize returns a human-readable file size string.
func FormatSize(b int64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%dB", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(b)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(b)/(1024*1024))
	}
}
