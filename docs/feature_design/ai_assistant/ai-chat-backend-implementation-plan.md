# AI Chat — Backend Implementation Plan

Audience: the backend engineer(s) who will turn the designed contract into code.

Goal: replace the current PoC (`POST /api/v1/ai-chat`, `POST /api/v1/ai-chat/stream`, stateless, dev-only) with a full-featured chat:

* per-user chat ownership, no cross-user visibility;
* durable storage in Postgres with a two-tier retention policy (TTL + "last M forever" + unlimited pins);
* chat CRUD (list/create/get/rename/pin-unpin/delete);
* streaming responses over SSE with tool-use visibility;
* downloadable files produced by the assistant, served from a generic `/api/v1/generated-files/{fileId}` endpoint via short-lived signed tokens;
* automatic context compaction when the conversation approaches the model's context window, so that old facts are re-packed into a summary rather than silently dropped by the LLM (the provider would otherwise fail the turn with `context_length_exceeded`);
* minimal FE↔BE and BE↔LLM-provider traffic.

Contract of reference: `docs/api/APIHUB_API.yaml` (tag **AI Chat**) and `docs/ai-chat-frontend-contract.md`.

---

## 1. Architecture overview

```text
controller/ChatController.go                     ──► HTTP/SSE layer (parse, auth, render)
controller/GeneratedFileController.go (new)      ──► /api/v1/generated-files/{fileId}
service/ChatService.go                           ──► orchestration: CRUD + turn pipeline
service/ChatContextService.go (new)              ──► history fetch, token counting, compaction
service/GeneratedFileService.go (new)            ──► backend-generated files on disk + signed tokens
service/cleanup/AiChat.go (new)                  ──► TTL-based chat cleanup job
service/cleanup/GeneratedFiles.go (new)          ──► /tmp file cleanup job
repository/ChatRepository.go (new)               ──► DB access
client/OpenAIClient.go                           ──► (existing) reused, but the Responses API
                                                     endpoints will be used instead of Chat Completions
```

Responsibilities split:

* `ChatController` only converts HTTP/SSE ↔ service calls; no business logic.
* `GeneratedFileController` owns the single public, token-authenticated endpoint `GET /api/v1/generated-files/{fileId}`. It is deliberately kept separate from `ChatController` because the route is not chat-scoped (the token alone authorises it) and may be reused by future non-chat features that produce downloadable files.
* `ChatService` owns transactions. A single "send message" call goes end-to-end through the service.
* `ChatContextService` hides the details of how the LLM-provider context is rebuilt for a turn (previous_response_id vs. summary+tail vs. full rebuild fallback).
* `GeneratedFileService` encapsulates the filesystem and defers all token minting/validation to the existing `security` package (see §6.3). No other code touches `/tmp`.
* `ChatRepository` is the only module that touches the three new tables.

---

## 2. Data model (Postgres)

New migration: `qubership-apihub-service/resources/migrations/34_ai_chat.up.sql` (with matching `*.down.sql`).

```sql
CREATE TABLE ai_chat (
    id                           uuid        PRIMARY KEY,
    user_id                      varchar     NOT NULL,
    title                        text        NOT NULL DEFAULT '',
    pinned                       boolean     NOT NULL DEFAULT false,
    created_at                   timestamp without time zone NOT NULL,
    -- Equal to created_at for a chat that has no messages yet; advances on every turn.
    -- Always populated (no NULLs) so that the list endpoint can sort uniformly.
    last_message_at              timestamp without time zone NOT NULL,
    messages_count               integer     NOT NULL DEFAULT 0,

    -- LLM-provider (OpenAI Responses API) thread head (see §5.2).
    openai_previous_response_id  text,
    -- Cursor above which messages are superseded by a summary (see §5.3).
    compacted_up_to_created_at   timestamp without time zone,
    -- The latest compaction summary (system-role text injected on next turn).
    compaction_summary           text,
    -- usage.total_tokens reported by the provider for the last assistant response on this chat,
    -- used to decide whether to compact before the *next* turn (see §5.3). NULL until the
    -- first turn completes or immediately after a compaction.
    last_turn_tokens             integer,

    CONSTRAINT ai_chat_user_fk FOREIGN KEY (user_id)
        REFERENCES user_data(user_id) ON DELETE CASCADE
);

CREATE INDEX ai_chat_user_sort_idx
    ON ai_chat (user_id, pinned DESC, last_message_at DESC);

CREATE INDEX ai_chat_retention_idx
    ON ai_chat (user_id, pinned, last_message_at);

CREATE TABLE ai_chat_message (
    id                  uuid        PRIMARY KEY,
    chat_id             uuid        NOT NULL
        REFERENCES ai_chat(id) ON DELETE CASCADE,
    role                varchar     NOT NULL,           -- 'user' | 'assistant'
    content             text        NOT NULL,
    client_message_id   uuid,                            -- idempotency key (user role only)
    tool_invocations    jsonb,                           -- [{name,status,durationMs}, ...]
    openai_response_id  text,                            -- assistant role only
    created_at          timestamp without time zone NOT NULL
);

CREATE INDEX ai_chat_message_chat_time_idx
    ON ai_chat_message (chat_id, created_at DESC);

CREATE UNIQUE INDEX ai_chat_message_client_id_idx
    ON ai_chat_message (chat_id, client_message_id)
    WHERE client_message_id IS NOT NULL;

CREATE TABLE ai_chat_file (
    id            uuid        PRIMARY KEY,
    chat_id       uuid        REFERENCES ai_chat(id) ON DELETE SET NULL,
    message_id    uuid        REFERENCES ai_chat_message(id) ON DELETE SET NULL,
    user_id       varchar     NOT NULL,
    filename      text        NOT NULL,
    storage_path  text        NOT NULL,
    mime_type     varchar,
    size_bytes    bigint,
    created_at    timestamp without time zone NOT NULL,
    expires_at    timestamp without time zone NOT NULL
);

CREATE INDEX ai_chat_file_expires_idx ON ai_chat_file (expires_at);
CREATE INDEX ai_chat_file_user_idx    ON ai_chat_file (user_id);
```

Down migration drops all three tables in reverse dependency order.

Notes on the columns:

* `pinned` is just a boolean — there is no separate `pinned_at`. The list endpoint sorts by `pinned DESC, last_message_at DESC`, which gives stable, intuitive ordering without an extra timestamp to maintain; the matching composite index is created below.
* `messages_count` is maintained by the service on every insert/delete (cheap, avoids a count(*) for the list endpoint).
* `tool_invocations` stores only UI-facing summaries (`name`, `status`, `durationMs`). Raw tool arguments and results are logged but not persisted — they are not needed for replay (the LLM has the final answer text).
* `openai_response_id` on assistant messages is the *per-message* ID. `openai_previous_response_id` on the chat row is the *current head*; it differs from the latest message's ID only right after a compaction (see §5.3).
* `compacted_up_to_created_at` marks the boundary: when replaying history for display we still return messages below the boundary (they remain visible); when rebuilding a fresh provider-side thread we skip them and use `compaction_summary` instead.
* No `context_compactions_count` column: compactions are a backend-internal implementation detail and not part of the wire contract. When we need operational visibility we read the Prometheus counter (§9) rather than a per-chat column.

---

## 3. Configuration

Extend `qubership-apihub-service/config/Config.go`:

Two values are **hardcoded identically on the client and on the server** and therefore are not stored in `config.yaml` and not exposed via any endpoint:

```go
const (
    MaxPinnedPerUser     = 3       // user-facing pin limit; FE mirrors the same constant
    MaxUserMessageLength = 32_000  // characters; generous headroom for JSON-schema / stacktrace pastes, still a DoS guard
)
```

Any future change of either constant has to be a coordinated FE + BE rollout — that is the whole point of baking them in instead of introducing a `/config` endpoint that every client would have to poll.

Everything else lives in `qubership-apihub-service/config/Config.go` as server-only knobs:

```go
type ChatConfig struct {
    OpenAI                     OpenAIConfig
    RetentionDays              int              // default 30
    PinnedForeverCount         int              // default 10
    CompactAtContextPercent    int              // default 80 — compact when used tokens ≥ this % of the model's context window
    CleanupSchedule            string           // cron, default "15 3 * * *"
    GeneratedFiles             GeneratedFilesConfig
}

type GeneratedFilesConfig struct {
    Directory       string // default os.TempDir()+"/apihub-ai-chat"
    TTLMinutes      int    // default 30
    CleanupSchedule string // cron, default "*/5 * * * *"
    MaxFileSizeMB   int    // default 50
}
```

Model context-window lookup is a constant map in `client/OpenAIResponsesClient.go` (e.g. `gpt-4o` → 128 000, `gpt-4o-mini` → 128 000, `gpt-4.1` → 1 047 576) and is refreshed when the model list evolves. `CompactAtContextPercent` is the single knob — "trigger compaction once ≥ X % of this model's window is used" — which keeps operators from thinking in absolute token numbers that become wrong whenever the model changes.

In `SystemInfoService`:

* add defaults via `viper.SetDefault` (see `setDefaults()` in `qubership-apihub-service/service/SystemInfoService.go`);
* exposed getters:
  * `GetAiChatRetentionPolicy() (retentionDays, pinnedForeverCount int)` — note: `maxPinnedPerUser` is *not* a runtime config, it is the compile-time constant above;
  * `GetAiChatGeneratedFilesConfig() config.GeneratedFilesConfig`;
  * `GetAiChatCompactAtContextPercent() int`;
* there is **no** `signedUrlSecret` — file download tokens are JWTs minted by the existing `security` package against the same RSA key that signs user session tokens (see §6.3). This means one signing key for the whole service; nothing new for operators to provision.
* feature-gate the whole chat under a new `ai.chat.enabled` bool that defaults to `false` in production.

Config sample (add to `config.yaml` example section in the deployment repository):

```yaml
ai:
  chat:
    retentionDays: 30
    pinnedForeverCount: 10
    compactAtContextPercent: 80
    cleanupSchedule: "15 3 * * *"
    generatedFiles:
      directory: "/tmp/apihub-ai-chat"
      ttlMinutes: 30
      cleanupSchedule: "*/5 * * * *"
      maxFileSizeMB: 50
    openAI:
      apiKey: "..."
      model: "gpt-4o-mini"
      # ...
```

---

## 4. HTTP / SSE layer

### 4.1 Routes

Register under `!productionMode` for now (same gate as the PoC) in `Service.go`:

```go
// /api/v1/ai-chat/* — full-featured AI chat
r.HandleFunc("/api/v1/ai-chat/chats", security.Secure(chatController.ListChats)).Methods(http.MethodGet)
r.HandleFunc("/api/v1/ai-chat/chats", security.Secure(chatController.CreateChat)).Methods(http.MethodPost)
r.HandleFunc("/api/v1/ai-chat/chats/{chatId}", security.Secure(chatController.GetChat)).Methods(http.MethodGet)
r.HandleFunc("/api/v1/ai-chat/chats/{chatId}", security.Secure(chatController.UpdateChat)).Methods(http.MethodPatch)
r.HandleFunc("/api/v1/ai-chat/chats/{chatId}", security.Secure(chatController.DeleteChat)).Methods(http.MethodDelete)

r.HandleFunc("/api/v1/ai-chat/chats/{chatId}/messages", security.Secure(chatController.ListMessages)).Methods(http.MethodGet)
r.HandleFunc("/api/v1/ai-chat/chats/{chatId}/messages", security.Secure(chatController.SendMessage)).Methods(http.MethodPost)
r.HandleFunc("/api/v1/ai-chat/chats/{chatId}/messages/stream", security.Secure(chatController.SendMessageStream)).Methods(http.MethodPost)

// public (no session cookie required), authorised by the signed token in the query string
r.HandleFunc("/api/v1/generated-files/{fileId}", security.NoSecure(generatedFileController.Download)).Methods(http.MethodGet)
```

Remove the two PoC routes (`POST /api/v1/ai-chat`, `POST /api/v1/ai-chat/stream`).

### 4.2 SSE framing

Use a dedicated helper (e.g. in `controller/sse.go`):

* headers: `Content-Type: text/event-stream; charset=utf-8`, `Cache-Control: no-cache`, `Connection: keep-alive`, `X-Accel-Buffering: no`;
* write `event: <type>\n` then `data: <json>\n\n` then `Flush()`;
* write a 15s heartbeat comment line (`: ping\n\n`) via a `time.Ticker` to keep idle connections alive behind nginx;
* on context cancellation (client abort), return — service will observe ctx.Done() and stop the OpenAI call.

The controller only emits pre-built DTOs received from the service. The service exposes a channel:

```go
type StreamEvent interface { EventType() string }

func (c *ChatService) SendMessageStream(ctx context.Context, userId, chatId string, req SendMessageRequest) (<-chan StreamEvent, error)
```

### 4.3 Request validation

* `content` — trim, non-empty, `len([]rune(content)) <= MaxUserMessageLength`;
* `clientMessageId` — optional UUID; if present, must be a valid UUID v4;
* ownership check: `chat.user_id == ctx.GetUserId()` — if not, return `404` (do not disclose existence of other users' chats);
* all 404s use `APIHUB-AI-3001`;
* pin validation: when `pinned: true` is requested in PATCH, count current pinned chats for the user and reject with `APIHUB-AI-4003` if already at `MaxPinnedPerUser` (the same constant the FE uses).

---

## 5. The turn pipeline

This is the heart of the service. The same pipeline is used for both streaming and non-streaming endpoints; the non-streaming wrapper just drains the channel and assembles the final DTO.

### 5.1 Steps

```text
sendMessage(ctx, userId, chatId, req):

1.   loadChat(ctx, userId, chatId)          -- locks chat via SELECT FOR UPDATE in a short tx
1a.  if req.clientMessageId != nil:
        existing := findAssistantByClientMessageId(chatId, clientMessageId)
        if existing: return replay(existing)     -- idempotency, no LLM call

2.   persistUserMessage(userMsg) in tx
     -- no event for this: reaching step 3 implies the message is saved;
     -- server-assigned id is not needed on the client stream (see FE contract).

3.   ctxSpec := chatContextService.Prepare(ctx, chat)
     if ctxSpec.WasCompacted:
         emit(context.compacted, {count})

4.   emit(message.assistant.start, {newMsgId})

5.   for each OpenAI iteration (tool-calling loop, same as PoC):
        if toolCall:
            emit(tool.started, {...})
            result := executeMCPTool(...)
            emit(tool.completed, {...})
        else (assistant content):
            for each delta chunk:
                emit(message.assistant.delta, {delta})

6.   persistAssistantMessage(asstMsg, tool_invocations, openai_response_id) in tx
     chat.openai_previous_response_id = asstMsg.openai_response_id
     chat.last_message_at = asstMsg.created_at
     chat.messages_count += 2
     save(chat) in tx

7.   emit(message.assistant.completed, {full message DTO})
     -- the DTO carries toolInvocations for UI transparency; raw tool payloads and
     -- provider usage are logged/metered server-side only (see §9), not shipped to the FE.
8.   emit(done)
```

Every channel emission blocks on `ctx.Done()` — the controller's cancellation cleanly tears down the pipeline.

Failure handling:

* any error before step 2 → regular HTTP error (the stream has not started yet);
* any error from step 3 onwards → emit `error` event and close the channel. If step 5 produced some assistant content, still persist it (truncated, clearly marked with a `[partial]` suffix in `content`) so history stays consistent. Log the full error.

### 5.2 OpenAI integration — Responses API

We switch from Chat Completions to the **Responses API** (`openai.Responses.New(ctx, ...)` in `openai-go v3`). This gives us server-side state: we pass only the new user message + `previous_response_id`, OpenAI reconstructs the thread.

Concrete wiring:

```go
params := responses.ResponseNewParams{
    Model:               shared.ChatModel(cfg.Model),
    PreviousResponseID:  openai.String(chat.OpenAIPreviousResponseID), // nil on first turn / after compaction
    Input: responses.ResponseNewParamsInputUnion{
        OfInputItemList: []responses.ResponseInputItemUnionParam{
            responses.ResponseInputItemParamOfMessage(
                responses.EasyInputMessageRoleUser, userMsg.Content,
            ),
        },
    },
    Store:            openai.Bool(true),
    Tools:            toolsAsResponsesTools(mcpTools),
    ReasoningEffort:  convertReasoningEffort(cfg.ReasoningEffort),
    // temperature/verbosity — same as today
}
```

On the very first turn the chat has no `openai_previous_response_id`, so `PreviousResponseID` is omitted and a system message is prepended (same content as today's `systemMessageBaseContent` + cached `api-packages-list`).

Tool-use loop: the Responses API also supports function tools. Reuse the MCP tool schema from `MCPService.MakeOpenAiMCPTools()` and adapt each to `responses.ResponseNewParamsToolUnionParam` (function tools). Tool outputs are fed back via `responses.ResponseNewParamsInputUnionOfInputItemList` with an `OfFunctionCallOutput` item referring to the corresponding `tool_call_id`. After the final (non-tool-call) response, capture its `id` and store it as the new `openai_previous_response_id`.

Streaming: use `client.Responses.NewStreaming(ctx, params)` — it exposes incremental events including `response.output_text.delta`, `response.function_call_arguments.delta`, `response.tool_call.completed` etc. Map these to our own SSE events.

Fallback path (response ID no longer accepted by OpenAI — e.g. 30-day retention):

* detect via the specific error code returned by the Responses API (`invalid_previous_response_id` or 404);
* rebuild context from our DB: apply the `compaction_summary` (if any) as a system message + all messages with `created_at > compacted_up_to_created_at`, send as a fresh `Input` list, drop `PreviousResponseID`. Save the new response ID.

### 5.3 Automatic context compaction

Rationale: the Responses API keeps the thread state on OpenAI's side, but when the accumulated thread exceeds the configured model's context window OpenAI returns `context_length_exceeded` and the turn fails — the aim of compaction is not cost saving, it is keeping old information alive in a shorter form before the model refuses to continue.

Triggered inside `ChatContextService.Prepare(ctx, chat)`:

1. Cheap estimate of currently used tokens (done **after** the previous turn, cached on `ai_chat.last_turn_tokens`):
   * if `openai_previous_response_id` is set, read `usage.total_tokens` from the previous response (we already have it from the stream — no extra call required);
   * otherwise estimate by running a local tokenizer (`tiktoken-go`) across the messages we would rebuild from.
2. Compute `threshold = modelContextWindow(cfg.Model) * CompactAtContextPercent / 100`.
3. If `lastTurnTokens + roughEstimate(newUserMsg) ≥ threshold`:
   a. load all messages of the chat (excluding those already compacted);
   b. call OpenAI once with a cheap model (e.g. `gpt-4o-mini`) asking for a structured summary (see prompt template below);
   c. within a single DB tx:
      * `chat.compaction_summary = <new summary>`;
      * `chat.compacted_up_to_created_at = <createdAt of the last message included in the summary>`;
      * `chat.openai_previous_response_id = NULL` (force next turn to start a fresh thread with the summary as a system message);
      * bump the `apihub_ai_chat_compactions_total` Prometheus counter (§9).
   d. return `{WasCompacted: true, MessagesCompacted: N}` so the pipeline can emit `context.compacted`.

Defensive safety net: if a turn still fails with `context_length_exceeded` despite the proactive trigger (e.g. a very large single user message that alone pushes us over), run the same compaction flow reactively once and retry the turn.

Summary prompt template (stored in code as a constant):

```text
You are compacting an ongoing conversation between a user and an API documentation
assistant so that the assistant can continue without losing essential context.
Produce a concise (≤ 1500 tokens) structured summary of the dialogue so far:
  • Goals the user is pursuing.
  • Facts / findings relevant to the task (package ids, operation ids, versions,
    filters the user cares about).
  • Decisions already made.
  • Open questions / TODOs.
Return plain prose, not JSON.
```

On the next turn the service sends the summary as a `responses.EasyInputMessageRoleSystem` input, followed by the new user message. `openai_previous_response_id` is set to the ID of that response; from then on turns go back to incremental mode until the next compaction.

Historical messages remain visible to the user (we never delete them mid-life), but they are filtered out when rebuilding OpenAI context.

### 5.4 Idempotency

Unique index on `(chat_id, client_message_id) WHERE client_message_id IS NOT NULL`. The service wraps step 2 in `INSERT ... ON CONFLICT (chat_id, client_message_id) DO NOTHING RETURNING id`. If nothing was returned, we fetch the existing user message and the assistant reply that follows it and replay that to the client (stream: emit all events in order; non-stream: just return the final DTO). No LLM call is made.

### 5.5 Auto-title

If `chat.title == ''` after the first assistant turn succeeds, schedule an async background task (fire-and-forget via `utils.SafeAsync`) that:

* makes a single non-streaming cheap-model call (e.g. `gpt-4o-mini`) with both the user question and the assistant answer and asks for "a 3-6 word title, no punctuation";
* updates `ai_chat.title` via a short tx;
* does not emit any event; the FE will see the updated title on the next `GET /chats` refresh.

If the background call fails, leave the title empty; the user can rename manually.

---

## 6. Generated files

Files produced by the assistant (exports, conversion results, generated diagrams) are exposed to the UI **exclusively as inline Markdown links** in the assistant's message body — there is no parallel `attachments` array on the wire (confirmed in the API spec and FE contract). The backend's job is to

1. store the file bytes under `/tmp`,
2. record a row in `ai_chat_file`,
3. mint a short-lived signed URL that the LLM receives as a plain string and embeds as `[filename](URL)` in its reply,
4. re-sign those URLs on subsequent `GET /messages` so that reloaded history keeps working until the file row expires.

### 6.1 Filesystem layout

Root: `cfg.GeneratedFiles.Directory` (default `<os.TempDir>/apihub-ai-chat`). Ensure the directory exists and is `0700` on startup.

Files are stored as `<root>/<userId>/<fileId>` (no original extension; the real filename and MIME are kept in the DB and served via `Content-Disposition`). Putting them under `<userId>/` makes manual ops trivially clear and allows per-user quotas in future.

### 6.2 Generation path

Files are expected to be produced by future MCP tools (out of scope for this iteration). The plumbing is:

* `GeneratedFileService.CreateFile(ctx, userId, chatId, messageId, filename, mimeType, reader)` — writes the bytes, inserts the `ai_chat_file` row with `expires_at = now + TTLMinutes`, and returns:

    ```go
    type GeneratedFile struct {
        ID         string
        Filename   string
        SizeBytes  int64
        ExpiresAt  time.Time
        URL        string // signed, ready for markdown embedding
    }
    ```

  `URL` is `/api/v1/generated-files/<fileId>?token=<jwt>` where `<jwt>` is minted with `ttl = time.Until(ExpiresAt)`.
* MCP tool adapters call `CreateFile` and return the resulting `URL` (and only the URL) back to the LLM as a plain string. The LLM embeds it verbatim, e.g. `[report.xlsx](/api/v1/generated-files/7f…?token=eyJ…)`. No structured attachment payload ever leaves the backend.
* When `GET /messages` is served, the service runs the rendered `content` through a single regular expression pass that matches `/api/v1/generated-files/<uuid>(\?token=[^)\s"]+)?`, extracts `<uuid>`, looks up the still-live row in `ai_chat_file` and substitutes a freshly-minted token. If the row is gone, the URL is **left as-is** — the browser will then get a clean `404 APIHUB-AI-3002` or `410 APIHUB-AI-4101` when the user actually clicks, which is the FE-contracted behaviour. Re-signing happens in memory; nothing is written back to `ai_chat_message.content`.

### 6.3 Signed tokens

File download tokens are **JWTs signed by the same RSA key the IdP already uses for user sessions** (`security/Auth.go`'s `keeper`). There is no separate HMAC secret — one signing key for the whole service, one piece of crypto state for operators to manage. The token type is deliberately generic (`generated-file-download`) so that the same endpoint and token-minting helpers can be reused by future non-chat features that also produce downloadable artefacts.

Scope isolation is provided by `TokenTypeExt`, the same mechanism that already keeps access and refresh tokens from being used in each other's place (`JWTValidator.ValidateToken` enforces type match). A new token type is introduced:

```go
// security/GeneratedFileTokens.go (new)
const GeneratedFileDownloadTokenType = "generated-file-download"

func MintGeneratedFileToken(userId, fileId string, ttl time.Duration) (string, error) {
    user := auth.NewUserInfo("", userId, nil, auth.Extensions{})
    ext := user.GetExtensions()
    ext.Set(TokenTypeExt, GeneratedFileDownloadTokenType)
    ext.Set("fileId", fileId)
    return jwt.IssueAccessToken(user, keeper, jwt.SetExpDuration(ttl))
}

func ValidateGeneratedFileToken(token string) (userId, fileId string, err error) {
    // Reuse the same JWTValidator that the rest of the auth layer uses.
    info, _, err := jwtValidator.ValidateToken(token, GeneratedFileDownloadTokenType)
    if err != nil {
        return "", "", err
    }
    return info.GetID(), info.GetExtensions().Get("fileId"), nil
}
```

Consequences, all of them desirable:

* **Cannot be used as a session token** — `BearerTokenStrategy` asks `JWTValidator.ValidateToken(tok, AccessTokenType)`, which rejects anything with a different `TokenTypeExt`. And vice versa: a real access token cannot be used as a file download token.
* **Revocation works for free.** `JWTValidator.parseAndValidate` already calls `IsTokenRevoked(userId, issuedAt)`. If the user is logged out / revoked, their outstanding file links die too — which is the correct behaviour. A subsequent `GET /messages` will re-sign fresh URLs after the user logs back in.
* **Token lifetime is decoupled from session lifetime.** We pass `ttl = time.Until(file.ExpiresAt)` — typically ≤ `TTLMinutes` (default 30 min).

Minor refactor required in `security/Auth.go`: `jwtValidator` is today a local variable inside `SetupGoGuardian`. Promote it to a package-level var (`var jwtValidator JWTValidator`) next to `keeper`, so the new file-token helpers can reuse the same validator instance (and therefore the same revocation service, the same leeway rules, etc.). No behaviour change for existing callers.

`GeneratedFileService` itself does not touch crypto; it calls `security.MintGeneratedFileToken` when constructing a `GeneratedFile` struct and `security.ValidateGeneratedFileToken` is called from `GeneratedFileController` on download.

Download flow (`GET /api/v1/generated-files/{fileId}?token=...`):

1. `security.ValidateGeneratedFileToken(token)` — on any failure (bad signature, type mismatch, issuer/audience mismatch, revoked) reject with `401`.
2. If the JWT `exp` is in the past, the validator returns a "token expired" error; map that to `410 Gone` + `APIHUB-AI-4101` (distinguished from generic `401` by inspecting the error).
3. Compare `fileId` from the token against the path parameter — mismatch ⇒ `401` (token for a different file).
4. Load `ai_chat_file` row by ID; if missing or already past `expires_at` → `404` + `APIHUB-AI-3002`. Also cross-check `row.user_id == tokenUserId` as a belt-and-braces measure (should always hold).
5. Open the file from disk, set `Content-Type` and `Content-Disposition: attachment; filename="<original>"`, stream bytes.

### 6.4 Cleanup job

New entry in `service/cleanup/GeneratedFiles.go`, registered in `main()` like the other cleanup jobs. Cron from `cfg.GeneratedFiles.CleanupSchedule` (default every 5 minutes):

1. `SELECT id, storage_path FROM ai_chat_file WHERE expires_at < now() LIMIT 1000`.
2. For each row, delete the file from disk (ignore ENOENT), delete the row.
3. Orphan sweep: walk the directory tree and remove any file whose ID is not in the DB (protects against crashes between FS write and DB insert).

---

## 7. Chat cleanup job

New `service/cleanup/AiChat.go`, cron from `cfg.Chat.CleanupSchedule` (default daily at 03:15). Per user:

1. Let `keepForever = pinnedForeverCount`. Compute the set of chat IDs to keep as:
   * all pinned chats;
   * top `keepForever` chats by `last_message_at DESC` among the non-pinned ones;
   * all non-pinned chats with `last_message_at > now() - retentionDays`.
2. Delete all other chats (cascading messages via `ON DELETE CASCADE`).

Done in one SQL statement per user to keep it cheap:

```sql
DELETE FROM ai_chat c
WHERE c.user_id = $1
  AND c.pinned = false
  AND c.last_message_at < now() - $2::interval
  AND c.id NOT IN (
      SELECT id FROM ai_chat
      WHERE user_id = $1 AND pinned = false
      ORDER BY last_message_at DESC
      LIMIT $3
  );
```

Driver query: `SELECT DISTINCT user_id FROM ai_chat`.

The job is protected by the existing `LockService` (same pattern as `CreateComparisonsCleanupJob`) so that only one backend instance in the cluster runs it at a time.

---

## 8. Responses API migration (openai-go/v3) — DONE

The chat path runs on `client.Responses.New` / `client.Responses.NewStreaming` end-to-end. No separate wrapper package was introduced — `service/ChatService.go` calls the SDK directly because the call sites are all internal:

* `runChatCompletionWithHistory(ctx, viewMessages, previousResponseID)` is the unstreamed primitive used by `ai_chat_service.runLLMTurn` for `POST /messages`. It:
  * always sends `Instructions` from `buildSystemMessage` (Responses API does not carry instructions across `previous_response_id`);
  * if `previousResponseID == nil`, sends the full `viewMessages` as `Input.OfInputItemList` (built by `convertViewMessagesToInputItems`, which turns each ChatMessage into an `EasyInputMessageParam`);
  * if `previousResponseID != nil`, sends only the new turn's items (the caller — `ai_chat_service` — passes a one-element `[]ChatMessage{user}`);
  * loops on function-call output items (`responses.ResponseInputItemParamOfFunctionCallOutput(callId, result)`) until the model produces text without function calls, advancing `PreviousResponseID = lastResp.ID` each iteration;
  * caps the loop at 10 iterations.
* `runChatCompletionStreaming(ctx, viewMessages, previousResponseID, hooks)` is the streaming twin used by `POST /messages/stream`. Same Responses-API semantics, but text deltas, tool-call lifecycles, and the final response ID are produced incrementally:
  * iterates `client.Responses.NewStreaming(...)`'s SSE union, switching on `event.Type`:
    * `response.output_text.delta` → forwards `event.Delta` via `hooks.OnTextDelta` (mapped to `message.assistant.delta` SSE on the public API);
    * `response.output_item.added` with type `function_call` → fires `hooks.OnToolStart` (mapped to `tool.started`) **as soon as the model commits to a tool**, well before arguments are fully assembled;
    * `response.output_item.done` with type `function_call` → collects the finalized call into the per-iteration `pendingCalls` slice;
    * `response.completed` → closes the iteration, captures `Response.ID` (next `previous_response_id`) and accumulates usage;
    * `response.failed` / `error` → bubble up.
  * after each iteration runs any pending tools synchronously via the existing `executeToolCallsWithInvocations`, fires `hooks.OnToolCompleted` per result, then feeds the outputs back as the next iteration's input;
  * stops when an iteration completes with no `pendingCalls`.
* MCP tools are converted to `responses.FunctionToolParam` with `Strict: false` (`convertToResponsesToolParams`).
* `generateChatTitle` and `summarizeMessagesForCompaction` are one-shot Responses calls with `Store: false` so they never appear in the user-visible chain.

Wiring in `ai_chat_service.runLLMTurn` (note the streaming/non-streaming branch):

```go
if !compacted && chat.OpenAIPreviousResponseID != nil && um != nil {
    prevResponseID = chat.OpenAIPreviousResponseID
    msgsForLLM = []view.ChatMessage{{Role: "user", Content: um.Content}}
} else {
    msgsForLLM = s.buildHistoryForLLM(chat, hist)  // first turn or post-compaction
}

if stream != nil {
    // SSE: forward deltas + tool lifecycle through hooks the moment they arrive
    turn, err = s.chat.runChatCompletionStreaming(ctx, msgsForLLM, prevResponseID, hooks)
} else {
    // POST /messages: get the whole assistant response in one go
    turn, err = s.chat.runChatCompletionWithHistory(ctx, msgsForLLM, prevResponseID)
}
chat.OpenAIPreviousResponseID = &turn.OpenAICompletionID  // head advances to the FINAL response in the chain
```

Compaction zeroes `OpenAIPreviousResponseID`, which forces the next turn to send the new compacted history (summary + recent tail) on a fresh thread.

**Recovery from invalid `previous_response_id`.** OpenAI eventually evicts stored responses from its server-side store; if our DB still references one, the next turn will fail. `runLLMTurn` detects this via `IsInvalidPrevResponseIDError` (matches `param=previous_response_id` plus relevant 400/404 patterns), zeroes `chat.OpenAIPreviousResponseID`, rebuilds the full compacted history with `buildHistoryForLLM`, and retries the turn once on a fresh thread. If the retry also fails, the error is surfaced to the client.

Version pinned in `go.mod` (`openai-go/v3 v3.31.0`).

---

## 9. Observability

* **Per-turn correlation ID.** `ai_chat_service.runLLMTurn` mints a fresh UUID for each user turn (covering all OpenAI calls in the tool loop) via `WithAiChatCorrelationID(ctx, ...)`. `service.openAIRequestOptions(ctx)` reads it back and attaches it to every `Responses.New` / `Responses.NewStreaming` call as the `X-Request-ID` header, so OpenAI's server-side traces line up with our log fields during incident triage.
* Structured `log.WithFields` for: `userId`, `chatId`, `messageId`, `turnDurationMs`, `promptTokens`, `completionTokens`, `compacted`, `toolCalls`, `streamClosedReason` (one of `done`, `error`, `client_aborted`).
* Prometheus counters/histograms in `metrics/`:
  * `apihub_ai_chat_turns_total{status="ok|error|aborted"}`;
  * `apihub_ai_chat_turn_duration_seconds`;
  * `apihub_ai_chat_turn_tokens{mode="stream|sync"}`;
  * `apihub_ai_chat_tool_calls_total{name,status}`;
  * `apihub_ai_chat_compactions_total`;
  * `apihub_ai_chat_generated_files_total`;
  * `apihub_ai_chat_generated_file_bytes`;
  * `apihub_ai_chat_cleanup_deleted_total{job}`.

---

## 10. Rollout and testing plan

### 10.1 Migration strategy

* The PoC had no persistence, so nothing to migrate.
* New migration `34_ai_chat.up.sql` adds three empty tables. On environments where the PoC is disabled this is a no-op for users.

### 10.2 Testing

1. **Unit tests**
   * Pin-limit enforcement in `ChatService.UpdateChat`.
   * Retention predicate in `cleanup/AiChat` (table-driven: various combinations of pinned/last-activity).
   * `security.MintGeneratedFileToken` / `security.ValidateGeneratedFileToken` — happy path, expired token, bad signature, wrong token type (e.g. a real access token must not validate as a file token), wrong `fileId` in the path vs token.
   * Link re-signing on `GET /messages` — stored markdown content with a stale token must come out with a fresh token; content pointing at an already-expired `ai_chat_file` row must be returned untouched.
   * Idempotency — two concurrent sends with the same `clientMessageId` should cause exactly one LLM call (simulated via a fake OpenAI client).
   * SSE framing — the controller writes well-formed frames for each event DTO.

2. **Integration tests** (use the existing docker-compose + test harness)
   * Full turn happy path with a fake OpenAI client that returns a scripted stream.
   * Auto-compaction: stub `GetResponseUsage` to return a tokens value above the threshold; verify `context.compacted` event fired and the chat row got a summary.
   * File download lifecycle: create a fake file via `GeneratedFileService`, assert the signed URL works before expiry, returns `410 APIHUB-AI-4101` after JWT expiry, `404 APIHUB-AI-3002` once the cleanup job has removed the row.
   * Ownership: user A cannot see user B's chats (tests return 404 on all CRUD attempts).

3. **Manual / end-to-end**
   * Dev-only deployment with FE integration.
   * Verify retention cleanup by setting `retentionDays=1`, `pinnedForeverCount=2` and advancing `last_message_at` manually in the DB.

### 10.3 Feature flag

Add `ai.chat.enabled` (default `false` in production, `true` in dev). Gate route registration in `Service.go` on this flag instead of the current `!productionMode` check, so we can progressively enable the feature per environment once ready.

---

## 11. Work-breakdown (proposed order)

1. **Config & scaffolding.** Extend `ChatConfig`, defaults, validation, new getters in `SystemInfoService`. Add a no-op `ChatService` interface shaped like the final one.
2. **DB migration + repository.** Write migration `34_ai_chat.up/down.sql`. Implement `ChatRepository` (CRUD + keyset pagination + idempotency insert).
3. **Chat CRUD endpoints.** Controller + service methods for `GET/POST /chats`, `GET/PATCH/DELETE /chats/{id}`, `GET /chats/{id}/messages`. No LLM involvement yet. Pin-limit validation uses the `MaxPinnedPerUser` compile-time constant.
4. **File download plumbing.** Add `security/GeneratedFileTokens.go` (mint + validate, reusing the IdP's JWT `keeper` and `JWTValidator`). Implement `GeneratedFileService` + `GeneratedFileController` + `GET /api/v1/generated-files/{fileId}` endpoint + cleanup job + link re-signing pass used by `GET /messages`. Can be tested without any MCP tool by injecting test files via an internal helper.
5. **Non-streaming send.** `POST /messages` wired through the Responses API (no compaction, no auto-title). Full tool-calling loop, one turn at a time.
6. **Streaming send.** SSE framing + `<-chan StreamEvent` service API. Reuse pipeline from step 5.
7. **Idempotency.** Plug `clientMessageId` uniqueness; add replay path.
8. **Auto-compaction.** `ChatContextService.Prepare`, compaction summary prompt, DB fields.
9. **Auto-title.** Background task after first turn.
10. **Retention cleanup job.** Implement, wire via `LockService`.
11. **Observability.** Metrics, structured logs, request ID propagation.
12. **Feature flag + remove PoC routes.** Flip the dev deployments to the new contract and delete the legacy endpoints + obsolete code in `service/ChatService.go` (the stateless `Chat` / `ChatStream` methods).

Each step produces a landable increment with tests; the FE team can start against step 3's shape (all CRUD is usable without real LLM traffic).
