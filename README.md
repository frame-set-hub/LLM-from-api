# LLM Terminal Chat Client

A **minimal, zero-boilerplate** Go terminal chat client for any **Anthropic-compatible LLM gateway**.  
Ships with a built-in conversation loop, **real-time streaming**, and **image (vision) support**.

---

## Features

- 🔐 Credential-free source code — secrets live only in `.env`
- 🧠 Full conversation memory — history is sent on every request
- ⚡ **Streaming responses** — tokens print as they arrive (no long silences)
- 🖼️ **Image support** — send local images (JPEG, PNG, GIF, WebP) with `/image`
- 🛠️ Slash commands: `/image`, `/reset`, `/history`, `/help`, `/quit`
- ⚙️ All config overridable via environment variables

---

## Project Structure

```
LLM-from-api/
├── .env                        # ← your secrets (git-ignored)
├── .env.example                # ← safe-to-commit template
├── .gitignore
├── go.mod / go.sum
├── main.go                     # REPL entry point + slash commands
├── README.md
├── CLAUDE.md                   # project guide for AI pair-programming
└── internal/
    ├── anthropic/
    │   └── client.go           # SSE streaming client (auth, JSON, image blocks)
    └── chat/
        └── session.go          # conversation-history manager
```

---

## Quick Start

### 1. Configure environment

```bash
cp .env.example .env
# Edit .env and fill in your real values
```

`.env` keys:

| Variable | Description |
|---|---|
| `ANTHROPIC_BASE_URL` | Gateway base URL |
| `ANTHROPIC_AUTH_TOKEN` | API key / auth token |
| `ANTHROPIC_DEFAULT_SONNET_MODEL` | Default model name |
| `LLM_MODEL` | *(optional)* Override model at runtime |

### 2. Run

```bash
go run .
```

### 3. Build a standalone binary

```bash
go build -o llm-chat .
./llm-chat
```

### 4. Override a single variable without editing .env

```bash
LLM_MODEL=other-model go run .
```

---

## Chat Commands

| Command | Effect |
|---|---|
| `/image <path> [caption]` | Send a local image file (with optional text) to the model |
| `/reset` | Clear conversation history |
| `/history` | Print all turns so far |
| `/help` | Show command list |
| `/quit` or `Ctrl+D` | Exit |

### Image example

```
You: /image ~/Desktop/chart.png What trend do you see in this chart?
```

Supported formats: **JPEG, PNG, GIF, WebP**

> **Note:** Vision support depends on the gateway. If the model or gateway doesn't support images, you'll get a clear API error message.

---

## Dependencies

| Package | Version | Purpose |
|---|---|---|
| `github.com/joho/godotenv` | v1.5.1 | Load `.env` file at startup |

All other logic uses Go stdlib only (`net/http`, `encoding/json`, `bufio`, `os`).

---

## Extending

- **REST API mode** — wrap `chat.Session` in a `net/http` handler or Gin router.
- **Multiple sessions** — store sessions in a `map[string]*chat.Session` keyed by session ID.
- **Persist history** — serialize `session.History()` to JSON after each turn.