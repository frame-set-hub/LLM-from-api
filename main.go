package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"llm-chat/internal/anthropic"
	"llm-chat/internal/chat"

	"github.com/joho/godotenv"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

const systemPrompt = "You are a helpful assistant."

// atPattern matches @filename or @/absolute/path tokens in user input.
var atPattern = regexp.MustCompile(`@([^\s]+)`)

// quotedPathPattern matches 'path' or "path" where path starts with / or ~/
// This covers drag-and-drop file paths on macOS.
var quotedPathPattern = regexp.MustCompile(`['"]((~|\/)[^'"]+)['"]`)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("[config] no .env file found — using OS environment variables only")
	}

	baseURL := requireEnv("ANTHROPIC_BASE_URL")
	apiKey := requireEnv("ANTHROPIC_AUTH_TOKEN")
	model := envOr("LLM_MODEL", requireEnv("ANTHROPIC_DEFAULT_SONNET_MODEL"))

	cfg := anthropic.DefaultConfig(baseURL, apiKey, model)
	client := anthropic.New(cfg)
	session := chat.NewSession(client, systemPrompt)

	// Working directory — files referenced by @name resolve relative to this.
	workDir, err := os.Getwd()
	if err != nil {
		workDir = "."
	}

	printBanner(model, baseURL, workDir)

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)

	for {
		fmt.Print("\nYou: ")

		if !scanner.Scan() {
			fmt.Println("\nGoodbye!")
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		// ---------------------------------------------------------------
		// Slash commands
		// ---------------------------------------------------------------
		switch {
		case input == "/quit" || input == "/exit" || input == "/q":
			fmt.Println("Goodbye!")
			return

		case input == "/reset" || input == "/clear":
			session.Reset()
			fmt.Println("[conversation history cleared]")
			continue

		case input == "/history":
			printHistory(session)
			continue

		case input == "/help":
			printHelp()
			continue

		case strings.HasPrefix(input, "/dir"):
			// /dir [path]
			target := workDir
			if parts := strings.Fields(input); len(parts) >= 2 {
				target = resolvePath(parts[1], workDir)
			}
			printDir(target)
			continue

		case strings.HasPrefix(input, "/cd "):
			newDir := resolvePath(strings.TrimPrefix(input, "/cd "), workDir)
			if info, err := os.Stat(newDir); err != nil || !info.IsDir() {
				fmt.Fprintf(os.Stderr, "[error] not a directory: %s\n", newDir)
			} else {
				workDir = newDir
				fmt.Printf("[working directory] %s\n", workDir)
			}
			continue

		case strings.HasPrefix(input, "/file "):
			// /file <path> [question...]  — explicit file attach
			args := strings.TrimPrefix(input, "/file ")
			parts := strings.SplitN(args, " ", 2)
			path := resolvePath(parts[0], workDir)
			question := ""
			if len(parts) == 2 {
				question = parts[1]
			}
			expanded, err := injectFile(path, question)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[error] %v\n", err)
				continue
			}
			input = expanded

		case strings.HasPrefix(input, "/image "):
			// /image <path> [caption...]
			args := strings.TrimPrefix(input, "/image ")
			parts := strings.SplitN(args, " ", 2)
			imagePath := resolvePath(parts[0], workDir)
			caption := ""
			if len(parts) == 2 {
				caption = parts[1]
			}
			fmt.Print("\nAssistant: ")
			if err := session.StreamWithImage(context.Background(), imagePath, caption, func(tok string) {
				fmt.Print(tok)
			}); err != nil {
				fmt.Fprintf(os.Stderr, "\n[error] %v\n", err)
			}
			fmt.Println()
			continue
		}

		// ---------------------------------------------------------------
		// File injection: @mention  or  '/path/file'  or  "/path/file"
		// ---------------------------------------------------------------
		resolved, injected, err := resolveAtMentions(input, workDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[error] %v\n", err)
			continue
		}
		resolved, injected2 := resolveQuotedPaths(resolved, workDir)
		injected = injected || injected2
		if injected {
			fmt.Printf("[injected file context into message]\n")
		}

		// ---------------------------------------------------------------
		// Send to model — streaming
		// ---------------------------------------------------------------
		fmt.Print("\nAssistant: ")
		if err := session.Stream(context.Background(), resolved, func(tok string) {
			fmt.Print(tok)
		}); err != nil {
			fmt.Fprintf(os.Stderr, "\n[error] %v\n", err)
		}
		fmt.Println()
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "scanner error: %v\n", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// File helpers
// ---------------------------------------------------------------------------

// resolvePath resolves p relative to workDir if it is not absolute.
func resolvePath(p, workDir string) string {
	if filepath.IsAbs(p) {
		return p
	}
	// Support ~ for home directory.
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, p[2:])
		return p
	}
	return filepath.Join(workDir, p)
}

// injectFile reads a file and wraps its content in a clear fenced block.
func injectFile(path, question string) (string, error) {
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

// resolveAtMentions scans input for @path tokens, reads each file, and
// replaces them with fenced code blocks.
func resolveAtMentions(input, workDir string) (string, bool, error) {
	matches := atPattern.FindAllStringSubmatchIndex(input, -1)
	if len(matches) == 0 {
		return input, false, nil
	}

	var sb strings.Builder
	prev := 0
	injected := false

	for _, m := range matches {
		fullStart, fullEnd := m[0], m[1]
		rawPath := input[m[2]:m[3]]

		path := resolvePath(rawPath, workDir)
		data, err := os.ReadFile(path)
		if err != nil {
			// Try appending common extensions (e.g. @main → @main.go)
			data, path, err = tryExtensions(path)
		}
		if err != nil {
			// Still not found — leave @token as-is.
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

// resolveQuotedPaths detects dragged-file paths like '/path/file' or "/path/file"
// and replaces them with fenced file content blocks.
func resolveQuotedPaths(input, workDir string) (string, bool) {
	matches := quotedPathPattern.FindAllStringSubmatchIndex(input, -1)
	if len(matches) == 0 {
		return input, false
	}

	var sb strings.Builder
	prev := 0
	injected := false

	for _, m := range matches {
		fullStart, fullEnd := m[0], m[1]
		rawPath := input[m[2]:m[3]]

		path := resolvePath(rawPath, workDir)
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

// tryExtensions tries common source file extensions on a base path.
func tryExtensions(base string) ([]byte, string, error) {
	exts := []string{".go", ".py", ".js", ".ts", ".md", ".json", ".yaml", ".yml", ".sh", ".txt"}
	for _, ext := range exts {
		p := base + ext
		if data, err := os.ReadFile(p); err == nil {
			return data, p, nil
		}
	}
	return nil, base, fmt.Errorf("file not found: %s", base)
}

// printDir lists the contents of a directory.
func printDir(path string) {
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
			fmt.Printf("  📄 %-40s %s\n", e.Name(), formatSize(info.Size()))
		}
	}
}

func formatSize(b int64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%dB", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(b)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(b)/(1024*1024))
	}
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

func requireEnv(key string) string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		log.Fatalf("[config] required environment variable %q is not set", key)
	}
	return v
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// ---------------------------------------------------------------------------
// UI helpers
// ---------------------------------------------------------------------------

func printBanner(model, baseURL, workDir string) {
	fmt.Println("┌─────────────────────────────────────────┐")
	fmt.Println("│          LLM Terminal Chat Client       │")
	fmt.Println("│      Streaming + Vision + File Access   │")
	fmt.Println("└─────────────────────────────────────────┘")
	fmt.Printf("  Model    : %s\n", model)
	fmt.Printf("  Gateway  : %s\n", baseURL)
	fmt.Printf("  WorkDir  : %s\n", workDir)
	fmt.Println()
	fmt.Println("Type your message. Use @filename to attach a file.")
	fmt.Println("Commands: /file  /image  /dir  /cd  /reset  /history  /help  /quit")
}

func printHelp() {
	fmt.Println()
	fmt.Println("File access:")
	fmt.Println("  @filename               — attach a file inline (e.g. @main.go วิเคราะห์โค้ด)")
	fmt.Println("  /file <path> [question] — attach a file with an optional question")
	fmt.Println("  /dir [path]             — list files in working directory (or given path)")
	fmt.Println("  /cd <path>              — change working directory")
	fmt.Println()
	fmt.Println("Other:")
	fmt.Println("  /image <path> [caption] — send an image to the model")
	fmt.Println("  /reset                  — clear conversation history")
	fmt.Println("  /history                — print conversation history")
	fmt.Println("  /help                   — show this help")
	fmt.Println("  /quit                   — exit")
	fmt.Println()
	fmt.Println("Tip: paths can be relative (to WorkDir), absolute, or start with ~/")
}

func printHistory(session *chat.Session) {
	history := session.History()
	if len(history) == 0 {
		fmt.Println("[no conversation history yet]")
		return
	}
	fmt.Println()
	for i, msg := range history {
		text := msg.ContentText()
		if len(text) > 200 {
			text = text[:200] + "…"
		}
		fmt.Printf("[%d] %s: %s\n", i+1, strings.ToUpper(string(msg.Role)), text)
	}
}
