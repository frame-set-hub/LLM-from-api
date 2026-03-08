package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"llm-chat/internal/anthropic"
	"llm-chat/internal/chat"

	"github.com/joho/godotenv"
)

// ---------------------------------------------------------------------------
// Configuration — all values come from .env (or OS environment).
// See .env.example for the full list of supported variables.
// ---------------------------------------------------------------------------

const (
	// systemPrompt is injected as the "system" field on every request.
	// Leave empty if you don't want a system prompt.
	systemPrompt = "You are a helpful assistant."
)

func main() {
	// Load .env file if present. OS-level env vars always take precedence.
	if err := godotenv.Load(); err != nil {
		log.Println("[config] no .env file found — using OS environment variables only")
	}

	baseURL := requireEnv("ANTHROPIC_BASE_URL")
	apiKey := requireEnv("ANTHROPIC_AUTH_TOKEN")
	// LLM_MODEL lets you override at runtime; falls back to the sonnet alias.
	model := envOr("LLM_MODEL", requireEnv("ANTHROPIC_DEFAULT_SONNET_MODEL"))

	cfg := anthropic.DefaultConfig(baseURL, apiKey, model)
	client := anthropic.New(cfg)
	session := chat.NewSession(client, systemPrompt)

	printBanner(model, baseURL)

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // support long pastes

	for {
		fmt.Print("\nYou: ")

		if !scanner.Scan() {
			// EOF (Ctrl+D)
			fmt.Println("\nGoodbye!")
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		// Built-in commands.
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

		case strings.HasPrefix(input, "/image "):
			// /image <path> [caption...]
			args := strings.TrimPrefix(input, "/image ")
			parts := strings.SplitN(args, " ", 2)
			imagePath := parts[0]
			caption := ""
			if len(parts) == 2 {
				caption = parts[1]
			}
			fmt.Print("\nAssistant: ")
			err := session.StreamWithImage(context.Background(), imagePath, caption, func(tok string) {
				fmt.Print(tok)
			})
			fmt.Println()
			if err != nil {
				fmt.Fprintf(os.Stderr, "\n[error] %v\n", err)
			}
			continue
		}

		// Normal text message — streamed.
		fmt.Print("\nAssistant: ")
		err := session.Stream(context.Background(), input, func(tok string) {
			fmt.Print(tok)
		})
		fmt.Println()
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n[error] %v\n", err)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "scanner error: %v\n", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// Helpers
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

func printBanner(model, baseURL string) {
	fmt.Println("┌─────────────────────────────────────────┐")
	fmt.Println("│          LLM Terminal Chat Client       │")
	fmt.Println("│         with Streaming + Vision 🖼️       │")
	fmt.Println("└─────────────────────────────────────────┘")
	fmt.Printf("  Model   : %s\n", model)
	fmt.Printf("  Gateway : %s\n", baseURL)
	fmt.Println()
	fmt.Println("Type your message and press Enter.")
	fmt.Println("Special commands: /image  /reset  /history  /help  /quit")
}

func printHelp() {
	fmt.Println()
	fmt.Println("Available commands:")
	fmt.Println("  /image <path> [caption]  — send an image file to the model")
	fmt.Println("  /reset                   — clear conversation history and start fresh")
	fmt.Println("  /history                 — print the current conversation history")
	fmt.Println("  /help                    — show this help message")
	fmt.Println("  /quit                    — exit the chat (also Ctrl+D or Ctrl+C)")
	fmt.Println()
	fmt.Println("Supported image formats: JPEG, PNG, GIF, WebP")
}

func printHistory(session *chat.Session) {
	history := session.History()
	if len(history) == 0 {
		fmt.Println("[no conversation history yet]")
		return
	}
	fmt.Println()
	for i, msg := range history {
		fmt.Printf("[%d] %s: %s\n", i+1, strings.ToUpper(string(msg.Role)), msg.ContentText())
	}
}
