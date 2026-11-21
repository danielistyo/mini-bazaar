# ZeroClaw patch: put the caller's identity on every MCP call

Status: not applied. This is the patch plan, traced against the source on the VPS
(`~/.zeroclaw/src`, version 0.9.0). ADR 0001 depends on it.

## What the backend needs

Every `tools/call` must carry:

```json
{ "name": "get_menu",
  "arguments": { "event_id": "BAZAR-2026-11-15" },
  "_meta": { "id.bazaar/context": {
    "channel": "whatsapp", "chat_ref": "...", "sender_ref": "..." } } }
```

`bazaar-mcp` already reads exactly this key and refuses when it is absent
(`mcpserver/scope.go`).

## What the source gives us

| Fact | Where | Consequence |
|---|---|---|
| `Tool::execute(&self, args)` has no context parameter | `zeroclaw-api/src/tool.rs` | identity cannot be passed as an argument |
| tools are called through `McpToolWrapper::execute` | `zeroclaw-tools/src/mcp_tool.rs:88` | the only place that can attach `_meta` |
| `McpRegistry::call_tool(name, arguments)` | `zeroclaw-tools/src/mcp_client.rs:728`, `:1254` | needs a third parameter, or `arguments` wrapping |
| request is serialized as `{"name", "arguments"}` | `mcp_client.rs:745`, `:870` | where `_meta` goes |
| wrappers are built without any chat identity | `zeroclaw-runtime/src/tools/scoped.rs:1294`, `tools/mod.rs:698` | identity is not available at construction |
| chat, sender and message id already exist per turn | `zeroclaw-channels/src/orchestrator/mod.rs:2438` | the values exist, one crate away |

So the gap is a crate boundary: the identity lives in the channel orchestrator,
the tool call happens in the tool stack, and the `Tool` trait has nowhere to put
it.

## Recommended patch

Use a task-local, not a new trait parameter. The turn runs on one tokio task per
message, so a task-local is correct, needs no signature changes, and cannot leak
between chats.

**1. `zeroclaw-api/src/tool.rs`** — define it next to the trait (~25 lines):

```rust
#[derive(Clone, Debug)]
pub struct TurnIdentity {
    pub channel: String,
    pub chat_ref: String,
    pub sender_ref: String,
}

tokio::task_local! {
    pub static TURN_IDENTITY: TurnIdentity;
}

pub async fn with_turn_identity<F: Future>(id: TurnIdentity, f: F) -> F::Output {
    TURN_IDENTITY.scope(id, f).await
}

pub fn turn_identity() -> Option<TurnIdentity> {
    TURN_IDENTITY.try_with(Clone::clone).ok()
}
```

**2. `zeroclaw-channels/src/orchestrator/mod.rs`** — around the agent turn call
in `run` / `process_message`, where `reply_target`, `sender` and `message_id` are
already in scope, wrap it:

```rust
let identity = TurnIdentity { channel, chat_ref: reply_target, sender_ref: sender };
with_turn_identity(identity, async { /* existing turn call unchanged */ }).await
```

**3. `zeroclaw-tools/src/mcp_tool.rs:88`** — read it and attach it:

```rust
let mut request = args;
if let Some(id) = turn_identity() {
    if let serde_json::Value::Object(map) = &mut request {
        map.insert("_meta".into(), json!({ "id.bazaar/context": id }));
    }
}
```

**4. `zeroclaw-tools/src/mcp_client.rs`** — split the object into `arguments` and
`_meta` when building the request, so `_meta` sits at the request level as the MCP
spec wants, not inside `arguments`:

```rust
let (arguments, meta) = match request {
    serde_json::Value::Object(mut map) => {
        let meta = map.remove("_meta");
        (serde_json::Value::Object(map), meta)
    }
    other => (other, None),
};
```

`json!({ "name": ..., "arguments": arguments, "_meta": meta })` at both
serializers (`:745`, `:870`). A `null` `_meta` must be omitted, so build the
object conditionally.

## Build and roll out

```sh
cd ~/.zeroclaw/src
cargo check                                    # fast; fix compile errors here
env CARGO_BUILD_JOBS=1 \
    CARGO_PROFILE_RELEASE_OPT_LEVEL=0 \
    CARGO_PROFILE_RELEASE_CODEGEN_UNITS=16 \
    CARGO_PROFILE_RELEASE_LTO=false \
    ~/.cargo/bin/cargo install --path . --locked --force \
    --no-default-features --features agent-runtime,gateway,whatsapp-web
```

Roughly 15 minutes, mostly the WhatsApp stack. The running bot keeps the old
binary until the tmux session is restarted.

## Verify

1. `go test ./mcpserver/` still passes with `--pilot-store` removed.
2. Start `bazaar-mcp` with no pilot flag and send one DM. The reply must arrive;
   the log must not show `no_store`.
3. In the same deployment, send from an unbound group. The reply must be the
   `no_store` refusal.
4. `zeroclaw channel list` still shows `WhatsApp Web`, unchanged.

If step 2 fails with `no_store`, the task-local is not in scope at the tool call:
the seam in step 2 is wrong, not the injection.

## Risks

- The orchestrator file is large. The scope must wrap the whole turn, including
  every tool call, or later iterations in the same turn lose the identity.
- A future ZeroClaw update overwrites all three edits. Re-apply them together with
  the PM-WA prefix filter; a missing patch makes the bot refuse everything, which
  is the safe failure.
