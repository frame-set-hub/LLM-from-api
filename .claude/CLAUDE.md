# CLAUDE.md — Project Guide for AI Pair-Programming

> This file gives an AI assistant (Claude, Gemini, GPT, etc.) everything it needs
> to understand, navigate, and extend this project correctly.

---

## Project Identity

| Field | Value |
|---|---|
| **Name** | `llm-chat` — LLM Terminal Chat Client |
| **Language** | Go 1.22+ |
| **Purpose** | Terminal REPL with SSE streaming and image (vision) support for an Anthropic-compatible LLM gateway |
| **Entry point** | `main.go` |
| **Module name** | `llm-chat` (see `go.mod`) |

---

## Directory Map

```
.
├── .env                        secrets (NEVER commit)
├── .env.example                template (always commit)
├── .gitignore
├── .claude/
│   ├── CLAUDE.md               this file
│   ├── settings.local.json     local permissions (git-ignored)
│   └── commands/               custom slash commands
│       ├── test.md             /test — run all tests
│       ├── build.md            /build — compile project
│       ├── lint.md             /lint — go vet + gofmt
│       ├── review.md           /review — review uncommitted changes
│       ├── coverage.md         /coverage — test coverage report
│       └── structure.md        /structure — show project architecture
├── go.mod / go.sum
├── main.go                     CLI entry: env loading, REPL loop, slash commands, UI
├── README.md                   human-facing quick-start
└── internal/
    ├── anthropic/
    │   ├── client.go           SSE streaming client — content blocks, image helpers
    │   └── client_test.go      tests: mock SSE server, headers, errors, cancellation
    ├── chat/
    │   └── session.go          stateful session: Stream(), StreamWithImage(), History()
    └── fileutil/
        ├── fileutil.go         path resolution, @mention injection, quoted paths, dir listing
        └── fileutil_test.go    tests: resolve paths, inject files, format sizes
```

---

## Core Logic Flow

```
main.go
  │
  ├── godotenv.Load()             load .env → os.Getenv
  ├── anthropic.New(cfg)          build HTTP client
  ├── chat.NewSession(client)     create session with empty history
  ├── signal.NotifyContext()      graceful shutdown on Ctrl+C / SIGTERM
  │
  └── REPL loop (ctx-aware)
        │
        ├── read line from stdin
        ├── handle slash commands locally:
        │     /file <path> [question]  → fileutil.InjectFile → send as text
        │     /image <path> [caption]  → session.StreamWithImage(ctx, ...)
        │     /dir /cd /reset /history /usage /help /quit
        ├── file injection:
        │     @mention             → fileutil.ResolveAtMentions
        │     '/path' or "/path"   → fileutil.ResolveQuotedPaths
        └── normal text input      → session.Stream(ctx, input, onToken)
              │
              ├── append user Message to history
              ├── anthropic.Client.Stream(ctx, history, systemPrompt, onToken)
              │     ├── POST /v1/messages  (stream: true)
              │     ├── headers: x-api-key, anthropic-version, content-type
              │     ├── parse SSE lines: "data: {...}"
              │     ├── call onToken(delta) for each content_block_delta
              │     ├── drain resp.Body on close (connection reuse)
              │     └── return accumulated full text + usage + error
              ├── append assistant Message to history
              └── return to REPL
```

---

## Environment Variables

| Variable | Required | Description |
|---|---|---|
| `ANTHROPIC_BASE_URL` | ✅ | Gateway base URL (trailing slash stripped automatically) |
| `ANTHROPIC_AUTH_TOKEN` | ✅ | API key / bearer token sent as `x-api-key` header |
| `ANTHROPIC_DEFAULT_SONNET_MODEL` | ✅ | Default model identifier |
| `ANTHROPIC_DEFAULT_HAIKU_MODEL` | ➖ | Alias — not used by CLI yet |
| `ANTHROPIC_DEFAULT_OPUS_MODEL` | ➖ | Alias — not used by CLI yet |
| `LLM_MODEL` | ➖ | Runtime override; takes priority over `ANTHROPIC_DEFAULT_SONNET_MODEL` |

---

## API Contract (Anthropic /v1/messages — streaming)

**Request headers:**
```
Content-Type: application/json
x-api-key: <ANTHROPIC_AUTH_TOKEN>
anthropic-version: 2023-06-01
```

**Request body (text):**
```json
{
  "model": "Qwen3.5-122B-A10B",
  "max_tokens": 8192,
  "stream": true,
  "system": "You are a helpful assistant.",
  "messages": [
    { "role": "user", "content": [{ "type": "text", "text": "Hello!" }] }
  ]
}
```

**Request body (image + text):**
```json
{
  "messages": [{
    "role": "user",
    "content": [
      { "type": "image", "source": { "type": "base64", "media_type": "image/jpeg", "data": "..." } },
      { "type": "text",  "text": "Describe this image." }
    ]
  }]
}
```

**SSE stream events we parse:**
```
event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}
```

---

## Key Design Decisions

| Decision | Rationale |
|---|---|
| No credentials in source | Security — `.env` is git-ignored |
| Full history per request | Simple stateless HTTP; gateway doesn't keep state |
| Content as `[]ContentBlock` | Supports both text and image blocks per Anthropic spec |
| `stream: true` + SSE parsing | Tokens arrive immediately; no silent wait for large models |
| `strings.TrimRight(baseURL, "/")` | Works whether `.env` has a trailing slash or not |
| Pop user msg on error | Caller can retry without duplicating history |
| `godotenv.Load()` non-fatal | Binary still works if deployed with OS env vars only |
| `internal/` packages | Hides implementation; only `main.go` is the public surface |
| `requireEnv()` helper | Fails fast with a clear message if a required var is missing |
| Graceful shutdown via context | Ctrl+C cancels in-flight HTTP requests cleanly |
| Drain `resp.Body` before close | Ensures HTTP connection reuse via keep-alive |
| SSE scanner with 256KB buffer | Prevents OOM from unexpectedly long SSE lines |
| `fileutil` package | Separates path/file logic from REPL — testable in isolation |

---

## How to Run

```bash
# Development
go run .

# Build
go build -o llm-chat .
./llm-chat

# With override
LLM_MODEL=other-model go run .

# Run tests
go test ./... -v

# Test coverage
go test ./... -coverprofile=coverage.out && go tool cover -func=coverage.out
```

---

## Claude Code Custom Commands

| Command | Description |
|---|---|
| `/test` | Run all tests with verbose output |
| `/build` | Compile and report errors |
| `/lint` | `go vet` + `gofmt` checks |
| `/review` | Review uncommitted changes |
| `/coverage` | Test coverage analysis |
| `/structure` | Show project architecture |

---

## Common Extension Patterns

### Add model switching at runtime
- Add `/model <name>` slash command in `main.go`.
- Store active model on `Session` and pass it through to `Client`.

### Expose as REST API
- Add `cmd/server/main.go`.
- Wrap `chat.NewSession` + `session.Stream` in a `POST /chat` handler.
- Use `sync.Map` to store sessions keyed by `session_id` header.

### Persist conversation history
- Serialize `session.History()` to JSON after each turn.
- Reload on startup from a `~/.llm-chat/history.json` file.

---

## Dependencies

```
github.com/joho/godotenv v1.5.1   — .env file loader (only external dep)
```

Everything else: Go stdlib (`net/http`, `encoding/json`, `bufio`, `os`, `encoding/base64`, `context`, `os/signal`).

---

## Conventions

- All packages under `internal/` — unexported, focused, testable in isolation.
- Error handling: wrap with `fmt.Errorf("context: %w", err)` everywhere.
- No global state — config flows through struct fields.
- Test files go beside source files: `client_test.go` next to `client.go`.
- `Session` is **not** safe for concurrent use — document if exposing via REST.
- Always update `README.md` and this file when changing code structure.
