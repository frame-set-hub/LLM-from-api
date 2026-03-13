package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"llm-chat/internal/anthropic"
	"llm-chat/internal/chat"
	"llm-chat/internal/fileutil"

	"github.com/joho/godotenv"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

const systemPrompt = "You are a helpful assistant."

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

	// Graceful shutdown: Ctrl+C cancels in-flight requests.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Working directory — files referenced by @name resolve relative to this.
	workDir, err := os.Getwd()
	if err != nil {
		workDir = "."
	}

	printBanner(model, baseURL, workDir)

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

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

		case input == "/usage":
			printUsage(session)
			continue

		case input == "/help":
			printHelp()
			continue

		case strings.HasPrefix(input, "/dir"):
			target := workDir
			if parts := strings.Fields(input); len(parts) >= 2 {
				target = fileutil.ResolvePath(parts[1], workDir)
			}
			fileutil.PrintDir(target)
			continue

		case strings.HasPrefix(input, "/cd "):
			newDir := fileutil.ResolvePath(strings.TrimPrefix(input, "/cd "), workDir)
			if info, err := os.Stat(newDir); err != nil || !info.IsDir() {
				fmt.Fprintf(os.Stderr, "[error] not a directory: %s\n", newDir)
			} else {
				workDir = newDir
				fmt.Printf("[working directory] %s\n", workDir)
			}
			continue

		case strings.HasPrefix(input, "/file "):
			args := strings.TrimPrefix(input, "/file ")
			parts := strings.SplitN(args, " ", 2)
			path := fileutil.ResolvePath(parts[0], workDir)
			question := ""
			if len(parts) == 2 {
				question = parts[1]
			}
			expanded, err := fileutil.InjectFile(path, question)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[error] %v\n", err)
				continue
			}
			input = expanded

		case strings.HasPrefix(input, "/image "):
			args := strings.TrimPrefix(input, "/image ")
			parts := strings.SplitN(args, " ", 2)
			imagePath := fileutil.ResolvePath(parts[0], workDir)
			caption := ""
			if len(parts) == 2 {
				caption = parts[1]
			}
			fmt.Print("\nAssistant: ")
			if err := session.StreamWithImage(ctx, imagePath, caption, func(tok string) {
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
		resolved, injected, err := fileutil.ResolveAtMentions(input, workDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[error] %v\n", err)
			continue
		}
		resolved, injected2 := fileutil.ResolveQuotedPaths(resolved, workDir)
		injected = injected || injected2
		if injected {
			fmt.Printf("[injected file context into message]\n")
		}

		// ---------------------------------------------------------------
		// Send to model — streaming
		// ---------------------------------------------------------------
		fmt.Print("\nAssistant: ")
		if err := session.Stream(ctx, resolved, func(tok string) {
			fmt.Print(tok)
		}); err != nil {
			if ctx.Err() != nil {
				fmt.Println("\n[interrupted — shutting down]")
				return
			}
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

func printUsage(session *chat.Session) {
	u := session.Usage()
	fmt.Println()
	fmt.Println("Token usage (this session):")
	fmt.Printf("  Input  : %d tokens\n", u.InputTokens)
	fmt.Printf("  Output : %d tokens\n", u.OutputTokens)
	fmt.Printf("  Total  : %d tokens\n", u.Total())
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
