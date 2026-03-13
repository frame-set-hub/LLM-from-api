# LLM Terminal Chat Client

A **minimal, zero-boilerplate** Go terminal chat client for any **Anthropic-compatible LLM gateway**.
Ships with a built-in conversation loop, **real-time streaming**, and **image (vision) support**.

---

## Features

- 🔐 Credential-free source code — secrets live only in `.env`
- 🧠 Full conversation memory — history is sent on every request
- ⚡ **Streaming responses** — tokens print as they arrive (no long silences)
- 🖼️ **Image support** — send local images (JPEG, PNG, GIF, WebP) with `/image`
- 📎 **File injection** — `@filename`, `/file`, drag-and-drop quoted paths
- 🛑 **Graceful shutdown** — Ctrl+C cancels in-flight requests cleanly
- 🛠️ Slash commands: `/file`, `/image`, `/dir`, `/cd`, `/reset`, `/history`, `/usage`, `/help`, `/quit`
- ⚙️ All config overridable via environment variables
- ✅ **Tested** — unit tests with mock SSE server for streaming, file injection, path resolution

---

## Project Structure

```
LLM-from-api/
├── .env                        # ← your secrets (git-ignored)
├── .env.example                # ← safe-to-commit template
├── .gitignore
├── .claude/                    # Claude Code project config
│   ├── CLAUDE.md               # AI pair-programming guide
│   └── commands/               # custom slash commands (/test, /build, /lint, etc.)
├── go.mod / go.sum
├── main.go                     # REPL entry point + slash commands + UI
├── README.md
└── internal/
    ├── anthropic/
    │   ├── client.go           # SSE streaming client (auth, JSON, image blocks)
    │   └── client_test.go      # mock SSE server tests
    ├── chat/
    │   └── session.go          # conversation-history manager
    └── fileutil/
        ├── fileutil.go         # path resolution, @mention injection, dir listing
        └── fileutil_test.go    # path, injection, and formatting tests
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

### 5. Run tests

```bash
go test ./... -v
```

---

## Chat Commands

| Command | Effect |
|---|---|
| `@filename` | Attach a file inline (e.g. `@main.go explain this code`) |
| `/file <path> [question]` | Attach a file with an optional question |
| `/image <path> [caption]` | Send a local image file (with optional text) to the model |
| `/dir [path]` | List files in working directory (or given path) |
| `/cd <path>` | Change working directory |
| `/reset` | Clear conversation history |
| `/history` | Print all turns so far |
| `/usage` | Show token usage for this session |
| `/help` | Show command list |
| `/quit` or `Ctrl+D` | Exit |

### File injection examples

```
You: @main.go explain the REPL loop
You: /file internal/anthropic/client.go what does Stream() do?
You: '/Users/me/Desktop/data.json' parse this
```

### Image example

```
You: /image ~/Desktop/chart.png What trend do you see in this chart?
```

Supported formats: **JPEG, PNG, GIF, WebP**

> **Note:** Vision support depends on the gateway. If the model or gateway doesn't support images, you'll get a clear API error message.

---

## Architecture

### Package Dependency

```mermaid
graph TD
    A[main.go<br><i>REPL + UI + Config</i>]
    B[internal/chat<br><i>Session + History</i>]
    C[internal/anthropic<br><i>SSE Client + Types</i>]
    D[internal/fileutil<br><i>Path + File Injection</i>]
    E[Anthropic-compatible<br>LLM Gateway]

    A --> B
    A --> D
    B --> C
    C -- "POST /v1/messages<br>(SSE stream)" --> E

    style A fill:#4a9eff,color:#fff
    style B fill:#34d399,color:#fff
    style C fill:#f59e0b,color:#fff
    style D fill:#a78bfa,color:#fff
    style E fill:#6b7280,color:#fff
```

### REPL Flow

```mermaid
flowchart TD
    Start([Start]) --> Load[Load .env + Config]
    Load --> Init[Create Client + Session]
    Init --> Signal[Register Signal Handler<br>Ctrl+C / SIGTERM]
    Signal --> Prompt[/Print 'You: '/]

    Prompt --> Read[Read stdin line]
    Read --> Empty{Empty?}
    Empty -- yes --> Prompt
    Empty -- no --> Slash{Slash command?}

    Slash -- "/quit" --> Exit([Goodbye!])
    Slash -- "/reset" --> Reset[Clear history] --> Prompt
    Slash -- "/history" --> ShowHist[Print turns] --> Prompt
    Slash -- "/usage" --> ShowUsage[Print tokens] --> Prompt
    Slash -- "/dir /cd" --> FileOp[Directory operation] --> Prompt
    Slash -- "/image path" --> ImgSend[StreamWithImage] --> Print1[Print response] --> Prompt
    Slash -- "/file path" --> Inject1[InjectFile into input]

    Slash -- "normal text" --> Mention["Resolve @mentions<br>+ quoted paths"]
    Inject1 --> Mention

    Mention --> Stream["session.Stream(ctx, input)"]

    Stream --> API["POST /v1/messages<br>stream: true"]
    API --> SSE["Parse SSE events"]
    SSE --> Token["onToken callback<br>(print each delta)"]
    Token --> Accumulate[Accumulate response + usage]
    Accumulate --> History[Append to history]
    History --> Prompt

    API -- "error / Ctrl+C" --> ErrHandle{Context<br>cancelled?}
    ErrHandle -- yes --> Exit
    ErrHandle -- no --> PrintErr[Print error] --> Prompt

    style Start fill:#4a9eff,color:#fff
    style Exit fill:#ef4444,color:#fff
    style Stream fill:#34d399,color:#fff
    style API fill:#f59e0b,color:#fff
    style SSE fill:#f59e0b,color:#fff
    style Token fill:#f59e0b,color:#fff
```

### SSE Streaming Detail

```mermaid
sequenceDiagram
    participant U as User (stdin)
    participant M as main.go
    participant S as chat.Session
    participant C as anthropic.Client
    participant G as LLM Gateway

    U->>M: "Hello!"
    M->>M: Resolve @mentions / quoted paths
    M->>S: Stream(ctx, input, onToken)
    S->>S: Append user Message to history
    S->>C: Stream(ctx, history, systemPrompt, onToken)
    C->>G: POST /v1/messages (stream: true)

    G-->>C: event: message_start (input_tokens)
    G-->>C: event: content_block_delta {"text":"Hi"}
    C-->>M: onToken("Hi")
    M-->>U: print "Hi"

    G-->>C: event: content_block_delta {"text":" there!"}
    C-->>M: onToken(" there!")
    M-->>U: print " there!"

    G-->>C: event: message_delta (output_tokens)
    G-->>C: event: message_stop

    C-->>S: return ("Hi there!", usage)
    S->>S: Append assistant Message to history
    S->>S: Accumulate token usage
    S-->>M: return nil
    M-->>U: newline + "You: " prompt
```

---

## Dependencies

| Package | Version | Purpose |
|---|---|---|
| `github.com/joho/godotenv` | v1.5.1 | Load `.env` file at startup |

All other logic uses Go stdlib only (`net/http`, `encoding/json`, `bufio`, `os`, `context`).

---

## Extending

- **REST API mode** — wrap `chat.Session` in a `net/http` handler or Gin router.
- **Multiple sessions** — store sessions in a `map[string]*chat.Session` keyed by session ID.
- **Persist history** — serialize `session.History()` to JSON after each turn.
- **Model switching** — add `/model <name>` slash command, store active model on `Session`.
