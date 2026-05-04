# IDS Generation — Feature Design

Audience: backend engineers extending the MCP-side authoring kits, operators who need to update the IDS template by rebuilding the image, anyone debugging an end-to-end "the chat produced a broken IDS doc" report.

Scope: how the AI chat assistant generates Integration Design Specification (IDS) documents on demand and delivers them to the user as downloadable Markdown files; where the template and generation prompt live; how MCP resources/prompts and chat-side tools collaborate; how to add new authoring kits in the future.

Prerequisite: this feature builds on the [AI chat assistant](./feature-ai-chat-design.md). Familiarity with the streaming flow, the OpenAI Responses API tool loop, and `GeneratedFileService` is assumed. Wire-level details for the surrounding pieces are not duplicated here.

---

## 1. User story

> **As an integration architect**, when I describe a 3rd-party integration scenario in the chat
> (e.g. "Create a design based on this text — CIP should call API Reserve_SIM_Profiles from TelCoopStock, version 2025.2 …"),
> I want the assistant to produce a complete Integration Design Specification document, look up the real API specs
> in APIHub for every operation I mention, and hand me back a downloadable `.md` file I can attach to a Jira ticket.

The expected interaction shape:

1. The user types a natural-language scenario into the chat.
2. The assistant streams a short progress narrative ("Looking up Retrieve_Quote in APIHub… Reading the IDS template…") with live tool pills.
3. The assistant's final reply is one paragraph + a Markdown link of the form `[IDS_TCS.md](/api/v1/generated-files/<id>?token=...)`.
4. The user clicks the link, the browser downloads the rendered IDS document.

The same flow has to work for external MCP clients (Claude Desktop, Continue, etc.) that connect to the apihub MCP server directly — they don't have a notion of "chat", but they get a first-class MCP **prompt** + **resource** they can drop into their own conversations.

## 2. Where the assets live

The template and the generation rules are static, image-bundled assets, not config or DB rows. The contract with operators is: **to update the IDS authoring rules or the template, edit the file in `resources/mcp/...` and rebuild the image** — no config knobs, no live reload, no DB migration.

```text
qubership-apihub-service/
└── resources/
    └── mcp/
        ├── prompts/
        │   └── ids_generation_prompt.md      ← step-by-step authoring rules (followed by the LLM)
        └── resources/
            └── ids_template.md               ← canonical IDS markdown template
```

The directory split mirrors the MCP protocol's own taxonomy:

* anything under `resources/mcp/resources/` is a static **MCP resource** (data the client can read);
* anything under `resources/mcp/prompts/` is a static **MCP prompt** (templated instruction set the client can render into messages).

The `Dockerfile` already copies `qubership-apihub-service/resources` verbatim into `/app/qubership-apihub-service/resources`, so the path resolves identically in local dev and in production.

Future authoring kits land by adding files into the same two directories. All resources are picked up automatically (see §4.1); prompts that take templated arguments need a one-liner registration in `MCPService.MakeMCPServer` that names the asset file and declares its arguments.

## 3. Architecture

```text
┌─────────────────────────────────────────────────────────────────────────────┐
│                       qubership-apihub-service (BE)                         │
│                                                                             │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │  MCPService (service/MCPService.go + MCPAssets.go)                    │  │
│  │  ─────────────────────────────────────────────────────────────────    │  │
│  │  • loadMCPAssets("./resources/mcp")  → in-memory snapshot at startup  │  │
│  │  • Auto-registers each file under resources/ as an MCP resource       │  │
│  │  • Registers `generate_ids_document` MCP prompt (user_input arg)      │  │
│  │  • IDSAuthoringKit(userInput) → assembles template+rules+input blob   │  │
│  └────────────┬──────────────────────────────────────────────────────────┘  │
│               │                                                             │
│               │ (in-process call)                                           │
│               ▼                                                             │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │  ChatService (service/ChatService.go + ai_chat_ids_tools.go)          │  │
│  │  ─────────────────────────────────────────────────────────────────    │  │
│  │  • Two extra OpenAI tools when assets are present:                    │  │
│  │    – start_ids_generation(user_input)  → calls IDSAuthoringKit        │  │
│  │    – save_generated_file(filename,content) → GeneratedFileService     │  │
│  │  • Routes the matching tool calls in executeToolCallsWithInvocations  │  │
│  └────────────┬──────────────────────────────────────────────────────────┘  │
│               │                                                             │
│               ▼                                                             │
│  ┌───────────────────────────────────────────────────────────────────────┐  │
│  │  GeneratedFileService → /tmp/<userId>/<fileId> + ai_chat_file row     │  │
│  │  + security.MintGeneratedFileToken (RS256, TTL=file.expires_at)       │  │
│  └───────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────┘
                                      ▲
                                      │  HTTPS + JSON-RPC
                                      │  (apihub MCP HTTP server, /api/v2/mcp)
                                      │
                  ┌───────────────────┴───────────────────┐
                  │  External MCP clients (Claude         │
                  │  Desktop / Continue / IDE plugins):   │
                  │  use the prompt + resource directly   │
                  └───────────────────────────────────────┘
```

Two consumption paths share the same backing store:

* the **internal AI chat** uses the in-process `MCPService.IDSAuthoringKit` via the `start_ids_generation` chat tool;
* **external MCP clients** use the public `generate_ids_document` MCP prompt and the `apihub://mcp/resources/ids_template.md` resource.

Both ultimately read the same files; the dual surface exists so APIHub's own UX has a polished one-click flow while MCP-aware tooling outside the portal can still consume the same assets in their native way.

## 4. Implementation walkthrough

### 4.1 Asset loader (`service/MCPAssets.go`)

`loadMCPAssets(rootDir)` is called once from `NewMCPService`. It reads `<rootDir>/prompts/*` and `<rootDir>/resources/*`, builds two in-memory maps keyed by the file's logical name (filename minus extension), and stores them on `mcpService.assets`.

Design choices:

* **Eager load, no live reload.** The image is the contract; runtime swap would defeat the whole point of "rebuild to ship a new template". The maps are read-only after startup.
* **Tolerant to missing directories.** A barebones deployment that doesn't ship any kits still starts; the dependent features (IDS tools, IDS prompt) just won't register, with a clear log line.
* **MIME type by extension.** `.md` → `text/markdown`, `.json` → `application/json`, `.yaml` / `.yml` → `application/yaml`, otherwise `text/plain`. Drives the MIME advertised on the public MCP resource.

Two helpers used downstream:

* `IDSAssetsAvailable()` → `true` only if both the template and the prompt are loaded. ChatService uses this to decide whether to advertise `start_ids_generation` to the model.
* `IDSAuthoringKit(userInput)` → assembles the LLM-facing instruction blob (see §4.4).

### 4.2 MCP-side registration (`service/MCPService.go::MakeMCPServer`)

```go
// Auto-register every resources/mcp/resources/<filename> as a static MCP resource.
for _, asset := range m.assets.ListResources() {
    s.AddResource(mcp.Resource{ URI: "apihub://mcp/resources/"+asset.Filename, ... },
        func(...) ([]mcp.ResourceContents, error) {
            return []mcp.ResourceContents{ &mcp.TextResourceContents{...} }, nil
        })
}

// IDS-specific prompt with a templated argument.
if m.IDSAssetsAvailable() {
    s.AddPrompt(mcp.Prompt{
        Name: "generate_ids_document",
        Arguments: []mcp.PromptArgument{{ Name: "user_input", Required: true, ... }},
    }, func(ctx, request) (*mcp.GetPromptResult, error) {
        kit, _ := m.IDSAuthoringKit(request.Params.Arguments["user_input"])
        return mcp.NewGetPromptResult("...", []mcp.PromptMessage{
            mcp.NewPromptMessage(mcp.RoleUser, mcp.NewTextContent(kit)),
        }), nil
    })
}
```

External MCP clients see two new entities:

* **Resource** `apihub://mcp/resources/ids_template.md` (MIME `text/markdown`) — the canonical IDS template;
* **Prompt** `generate_ids_document` with one required argument `user_input` — returns a single user-role message containing the full authoring kit.

The auto-registration of resources is generic — every file dropped under `resources/mcp/resources/` is published the same way, so adding a second authoring kit's template is a pure file-system change.

### 4.3 Chat-side tools (`service/ai_chat_ids_tools.go`)

The AI chat exposes two extra OpenAI tools to the model, but **only when the IDS assets actually loaded**. If the image was built without `resources/mcp/...`, the tools are not advertised at all — there's no point tempting the LLM to call something that would error out. The list of tools the model sees is composed at startup in `NewChatService`:

```go
mcpTools := mcpService.MakeOpenAiMCPTools()                    // search/getSpec/diff
if mcpService.IDSAssetsAvailable() && generatedFiles != nil
    && mintFileToken != nil {
    mcpTools = append(mcpTools, makeIDSChatTools()...)         // start_ids_generation + save_generated_file
}
```

#### `start_ids_generation(user_input)`

Pure facade over `MCPService.IDSAuthoringKit`. Runs synchronously, returns a single `TextContent` with the assembled instruction blob. Bounds the input at 64 KiB so a runaway model cannot drag the whole context window into a tool argument.

#### `save_generated_file(filename, content)`

The capstone of the flow. Persists the model-produced Markdown body and returns a download link.

Important wiring:

* The tool **does not take userID / chatID as arguments**. They flow through `context.Context` via `WithAiChatTurn`, which `AiChatService.runLLMTurn` populates at the start of every turn. Removes a class of mistakes where the LLM hallucinates an owner.
* `GeneratedFileService` and `security.MintGeneratedFileToken` are injected into `ChatService` at construction (see `Service.go::NewChatService`); the tool calls them directly.
* The link returned to the model is the ready-to-embed `[<filename>](<apihubURL>?token=<jwt>)`. Token TTL is anchored to `row.ExpiresAt` (set by `GeneratedFileService` from `ai.chat.generatedFiles.ttlMinutes`).
* The tool result is a small JSON object with `markdown`, `url`, `fileId`, `expiresAt`, `sizeBytes`, plus an explicit `instruction` field telling the model exactly what to do with the link. This is belt-and-suspenders against the model paraphrasing the URL or losing the token.

Filename sanitisation is intentionally aggressive: the model gets the ASCII subset `[A-Za-z0-9._-]`; everything else collapses to `_`. `GeneratedFileService` also sanitises (defence in depth) but having the chat-side cleaner produces a cleaner DB row.

### 4.4 The authoring kit (`MCPAssets.IDSAuthoringKit`)

This is the brain of the feature. The function takes the user's natural-language request and produces a single string the LLM consumes as the spec for the rest of the turn. Layout (annotated):

````text
You are now generating an Integration Design Specification (IDS) ...
... use search_rest_api_operations / get_rest_api_operations_specification ...
... when done, call save_generated_file ...

## USER REQUEST (verbatim)
<userInput>

## TEMPLATE TO FILL (markdown)
```markdown
<contents of resources/mcp/resources/ids_template.md>
```

## GENERATION RULES (step by step)
<contents of resources/mcp/prompts/ids_generation_prompt.md>

## OUTPUT CONTRACT
- Produce the full IDS document ...
- Pick a concise filename like `IDS_<3rdPartySystemAbbrev>.md` ...
- Call `save_generated_file` ...
````

Why this shape:

* **One blob, one tool.** The model only needs to learn one path: "call `start_ids_generation`, follow the result, call `save_generated_file`". No multi-step orchestration on the LLM side.
* **Verbatim quotes** of the template and the rules — both are authored by domain experts and shouldn't be reworded.
* **Explicit hand-off contract** at the end so the model finalises the flow with `save_generated_file` instead of dumping the whole IDS body into chat (which would defeat the "downloadable file" UX).

## 5. End-to-end flow

```mermaid
sequenceDiagram
    participant FE
    participant Svc as AiChatService
    participant Chat as ChatService
    participant MCP as MCPService
    participant GF as GeneratedFileService
    participant OAI as OpenAI

    FE->>Svc: POST /messages/stream<br/>"Create a design based on this text..."
    Svc->>Svc: persist user message; ctx ← WithAiChatTurn(userID, chatID)
    Svc-->>FE: SSE message.assistant.start
    Svc->>Chat: runChatCompletionStreaming(history, prevID, hooks)

    Chat->>OAI: Responses.NewStreaming(req)
    OAI-->>Chat: response.output_item.added (function_call: start_ids_generation)
    Chat-->>FE: SSE tool.started (start_ids_generation)
    OAI-->>Chat: response.output_item.done (args={user_input})
    Chat->>MCP: IDSAuthoringKit(user_input)
    MCP-->>Chat: kit (template+rules+input)
    Chat-->>FE: SSE tool.completed (start_ids_generation)

    Chat->>OAI: Responses.NewStreaming(function_call_output, prevID)
    note over OAI: model walks APIHub<br/>per the kit's rules
    OAI-->>Chat: search_rest_api_operations / get_rest_api_operations_specification calls
    Chat->>MCP: ExecuteSearchTool / ExecuteGetSpecTool
    MCP-->>Chat: tool results (operations + specs)
    Chat-->>FE: SSE tool.started / tool.completed * N

    OAI-->>Chat: response.output_item.done (function_call: save_generated_file)
    Chat->>GF: SaveFile{userID, chatID, filename, mime=text/markdown, body}
    GF-->>Chat: row {ID, ExpiresAt}, relURL
    Chat->>Chat: MintGeneratedFileToken(userID, fileID, ttl)
    Chat-->>FE: SSE tool.completed (save_generated_file)

    Chat->>OAI: Responses.NewStreaming(function_call_output={markdown, url, ...})
    OAI-->>Chat: response.output_text.delta * N (final summary + the [filename](url) link)
    Chat-->>FE: SSE message.assistant.delta * N
    Chat-->>Svc: chatTurnResult { content: "Here is the IDS… [IDS_TCS.md](url)" }
    Svc-->>FE: SSE message.assistant.completed + done
```

The whole thing is **one chat turn** — the user sends one message, the assistant streams back text and pills, the final reply contains the link. From the FE's perspective it's the same shape as any other turn (no special-casing needed); the only visible difference is the live `start_ids_generation` and `save_generated_file` pills.

## 6. Why this split (design rationale)

A few choices that may not be obvious from the code:

* **Template + rules as separate files**, not one combined file. The template is shared verbatim with external MCP clients as a Resource — they need it free of the per-step rules. The rules then reference the template by content (the kit assembler glues them together).
* **`start_ids_generation` is a chat tool, not an MCP tool.** The same instruction blob is exposed via the MCP `generate_ids_document` prompt for external clients. We don't double-publish it as an MCP tool because (a) tool calls in MCP are designed for actions with side effects, and (b) the tool's result is purely instructional — that's literally what prompts are for.
* **`save_generated_file` is a chat tool, not an MCP tool.** The download URL it returns (`/api/v1/generated-files/<id>?token=<jwt>`) is only meaningful inside the apihub backend. External MCP clients have no apihub session and no way to consume the URL; exposing the tool to them would mislead. They get the kit and produce the file in their own client-side flow.
* **Synchronous tool execution inside the streaming loop.** `save_generated_file` could in principle have been async (return a "saving…" handle and resolve later), but the LLM's UX of "tool returns the link, model embeds it in the next sentence" is much cleaner if the call is synchronous and finishes within a few hundred milliseconds. A 2 MiB cap on the body keeps the write fast even on slow disks.
* **No new database tables.** The feature reuses `ai_chat_file` for storage and the existing RS256 keeper for token signing. The IDS template/prompt are *not* in the DB on purpose — they are part of the binary artefact (image), not user-mutable data.

## 7. Operational notes

* **Feature flag inheritance.** The IDS feature inherits the AI chat's master kill-switch (`ai.chat.enabled`).
  When the chat is off, neither the chat tools nor the MCP prompt/resource are reachable through the normal entry
  points. The `MCPService.MakeMCPServer` registration is independent of the kill-switch — i.e. the MCP HTTP endpoint
  still publishes the resources/prompt — but the apihub MCP server is itself a separate route that operators can
  choose to expose or not.
* **Updating the template / rules.** Edit `qubership-apihub-service/resources/mcp/{resources,prompts}/...`, rebuild the image, redeploy. There is no live reload; this is by design (audit trail = Git history of the resource files + image SHA).
* **Removing the feature.** Delete the two files. On startup, `MCPAssets` will log a warning, `IDSAssetsAvailable()` returns false, the chat tools are not registered, and the MCP prompt is not published. No code changes needed.
* **Adding another authoring kit.** Drop a new template under `resources/mcp/resources/<name>.md`
  (auto-registered as MCP resource), drop a new rules file under `resources/mcp/prompts/<name>.md`, then add ~30
  lines mirroring the IDS pattern: a constant pair of asset names, a tiny `<New>AuthoringKit` method on `mcpAssets`,
  a `<New>Tools` factory on the chat side, and a registration of the MCP prompt in `MakeMCPServer`.
  No DB, no migration, no config.

## 8. Limitations & known gaps

* **Single-shot generation.** The model produces the entire IDS in one go; there is no checkpoint mechanism for
  very long documents. Empirically, the template + a 2-3 operation scenario fits comfortably in one turn even at
  `verbosity=high`. If a future template balloons or operators want multi-pass refinement, the natural extension is
  to make `save_generated_file` accept a `mode = draft|final` flag and let the model iterate.
* **No verification that APIHub lookups actually happened.** The rules tell the model `MUST` look up every API; if the model misbehaves, the only safety net is the user's review. A stricter implementation could enforce "for each `Operation:` section in the document, there must be at least one `get_rest_api_operations_specification` invocation in `toolInvocations`", but it is not implemented today.
* **Filenames are de facto English/ASCII.** `sanitizeChatToolFilename` collapses non-ASCII to `_`. Cyrillic / CJK filenames are not preserved through the chain. This matches today's IDS naming conventions (`IDS_<SystemAbbrev>.md`).
* **Templates are not versioned in MCP resource metadata.** External MCP clients see the latest content but no version string. If multiple template revisions need to coexist, the natural approach is to publish each as `apihub://mcp/resources/ids_template_v<N>.md`.

## 9. References

* Implementation files:
  * `qubership-apihub-service/resources/mcp/prompts/ids_generation_prompt.md` — the rules.
  * `qubership-apihub-service/resources/mcp/resources/ids_template.md` — the template.
  * `qubership-apihub-service/service/MCPAssets.go` — loader + kit assembler.
  * `qubership-apihub-service/service/MCPService.go` — public MCP registration.
  * `qubership-apihub-service/service/ai_chat_ids_tools.go` — chat-side tools.
  * `qubership-apihub-service/service/ai_chat_observability.go` — `WithAiChatTurn` ctx helper.
* Companion docs:
  * [AI Chat Assistant — Feature Design](./feature-ai-chat-design.md) (architecture this builds on).
  * [AI Chat — Frontend Contract](./ai-chat-frontend-contract.md) (no FE changes were required for IDS; the link mechanism reuses the existing `[name](url)` contract).
  * [AI Chat — Backend Implementation Plan](./ai-chat-backend-implementation-plan.md).
