# AGENTS.md

Instructions for AI agents working in this repository.

Deployment values (bot number, group JIDs, host, ssh user, checkout path) are replaced by placeholders such as `<bot-number>`. If `AGENTS.local.md` exists, read it for the real values; it is gitignored.

## What this is

Bazar KPKM Order Assistant: a WhatsApp bot that records food and drink orders for a bazaar team, lets the team edit or cancel them, tracks payment, and produces summaries.

- **ZeroClaw** is the WhatsApp and agent runtime.
- **A local Go MCP server** owns the order logic and exposes tools over stdio.
- **SQLite** is the single source of truth for events, customers, orders, payments, and history.

Status: live on 2026-11-12. Go MCP server seeded; ZeroClaw 0.9.0 with WhatsApp Web is paired as `<bot-number>` and patched to drop PM-WA commands. DMs are on; allowed groups are `Pengurus KPKM Awet Muda` and `Sie bazzar`. Target: **Bazar KPKM, Sunday 15 November 2026**. Menu: per event, backed by a shared catalog; the live event has 29 products.

## Hard rules

1. SQLite is the only source of truth for orders. Never answer counts or totals from conversation history.
2. The LLM never calculates prices, subtotals, or totals. Tools do integer Rupiah math and return a `formatted` text block.
3. Every write is two-phase: create a Draft, show it, receive an explicit `OK <4-digit short_code>`, then `commit_draft`. A plain "yes" is never confirmation.
4. Never hard-delete an Order. Cancellation is a status change; the order and its audit log stay.
5. Every Order belongs to exactly one Event. Never mix events. If no event is named, use the Active Event and name it in the preview.
6. Cutoff is informational. Never reject or delay an order because the cutoff passed.
7. No WhatsApp JIDs or phone numbers in SQLite. Customers are names only. Similar names are confirmed, never merged silently.
8. Never invent a Product, price, or alias. Ambiguous product or customer means ask.
9. One confirmed Order per (Event, Customer). A second request is an edit/append to the existing Order. A Cancelled Order does not block a new one.
10. Payment is separate from order status. Editing a Paid Order resets it to Unpaid, and the preview says so.
11. The WhatsApp group is a trusted team group. Confirmation is a UX guard, not a security boundary: the LLM supplies tool arguments, and ZeroClaw does not pass trusted sender identity to MCP tools.
12. Text orders only. Images, voice notes, and quoted message text are not parsed by the WhatsApp adapter.

## Architecture

```
WhatsApp: team group + organizer DM
        │
   ZeroClaw: WhatsApp Web, mention-only, passive group context
        │  MCP over stdio
   Go MCP server: bazaar-mcp
        │
   SQLite: data/orders.db
```

- ZeroClaw v0.8.7 or newer (Rust). WhatsApp Web adapter with a persistent session path.
- LLM: OpenCode Go, model `deepseek-v4.1-flash` (DeepSeek V4.1 Flash 1017; tool calling confirmed). Fallback: `glm-5.3-flash`; the provider block is swappable.
- MCP server: Go 1.25+, module `bazaar-minggu`, official MCP Go SDK v1.8+, `database/sql` with `mattn/go-sqlite3` (same driver as PM-WA). Single binary, no ORM, no web server.
- Database: SQLite in WAL mode, foreign keys on, `busy_timeout` set, `SetMaxOpenConns(1)`. One writer process.
- Timezone: `Asia/Jakarta` (WIB, UTC+7). Event dates are WIB calendar dates (`YYYY-MM-DD`); instants are Unix seconds (`int64`) and displayed in WIB.

### Why an MCP server

ZeroClaw routes WhatsApp messages and runs the LLM; it does not own order state. The MCP server is the only component allowed to write SQLite or calculate money. It runs as a local stdio child process spawned by ZeroClaw — no port, no daemon, no separate hosting.

Rejected alternatives:

- **Raw shell + SQLite:** the LLM would write SQL and calculate totals, with no transactions, validation, or audit guarantee.
- **Custom ZeroClaw Rust tools:** more code, compiled, and coupled to ZeroClaw's internals.
- **CLI wrapper via the shell tool:** workable, but the LLM builds shell commands and parses string output; structured tool schemas and errors are cheaper over MCP.

The order logic (schema, validation, math, formatting) is needed either way. MCP is the thin contract around it.

### Repository layout

```
cmd/bazaar-mcp/main.go   subcommands: serve, init, seed-menu
db/                      open, migrations, queries, transactions
orders/                  domain rules and tool logic
format/                  Rupiah and WhatsApp text
mcpserver/               MCP tool registration and thin handlers
textnorm/                customer and product normalization
prompts/system.md        ZeroClaw agent instructions
menu.csv                 owner-provided menu
data/orders.db           SQLite, gitignored
```

Tests sit next to the package they test (`db/db_test.go`, `orders/draft_test.go`) in external `_test` packages.

### Go conventions

- `go.mod` targets `go 1.25` and module `bazaar-minggu`; the VPS runs Go 1.25.9.
- `log/slog` for logging, errors returned, no panics in library code.
- `mattn/go-sqlite3` needs CGO. Build on the VPS (PM-WA already builds CGO SQLite there) or locally with Xcode CLT.
- Money is `int64` Rupiah. Times are Unix seconds; format in WIB with `time.FixedZone("WIB", 7*60*60)`.
- Thin MCP handlers: SQL in `db/`, policy in `orders/`, text in `format/`.
- In `serve`, logs go to stderr. stdout is the MCP JSON-RPC channel; never print to stdout.
- Open SQLite exactly like PM-WA: `file:<path>?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON`, then `SetMaxOpenConns(1)`.
- Migrations use PM-WA's `schema_version` table pattern.
- Table-driven tests in external `_test` packages; `t.TempDir()` for SQLite.
- `go vet ./...` is the lint gate; `go test ./...` is the test gate.

## Domain rules

### Events

- Initial event: code `BAZAR-2026-11-15`, name "Bazar KPKM", date `2026-11-15` (Sunday), cutoff `2026-11-14T12:00:00+07:00` (Saturday noon). Cutoff is displayed only.
- Exactly one Active Event at a time. New orders default to it.
- `create_event` creates an inactive event and copies the active event's listed menu at the active event's prices. `set_active_event` switches the active event; the bot announces the new one.
- Drafts store their event at creation, so switching events cannot move an in-flight draft.
- Test events are normal events. Their orders never appear in another event's summary.

### Menu

- Each event has its own menu. Products belong to a shared catalog (name, category, aliases); price and listing live in `event_products` per event.
- Seed data lives in `menu.csv` (`category,name,price,aliases`). `make seed-menu` applies the CSV to the Active Event, not to every event. Cincau Station is split into four flavors: Gurin, Orenji, Original, Honey Lime (Rp12.000 each), so a flavor can sell out independently.
- The live event has 29 products, including Lauk ayam mboksiah (Rp20.000).
- The price is snapshotted when a Draft is created and carried into the confirmed Order. Later price changes, even in the same event, never alter existing Orders.
- Aliases are suggestions shared across events. `find_product` returns candidates from the event's menu only; the bot asks when more than one matches.
- Sold-out is temporary and per Event (`event_sold_out`). Use `set_product_available`.
- `set_product_enabled` lists or removes a product on one event's menu. Removing keeps the row and its price, hides it from `get_menu`, marks it unavailable in `find_product`, and makes new orders fail with `sold_out`. Other events are untouched. History is never deleted.
- `upsert_product` adds a catalog product and lists it on one event, or updates the shared name, category, and aliases plus that event's price and listing. Name clash returns `conflict` with the existing id. Menu admin, organizer DM only.
- `products.is_available` is a dormant global kill used only by operations; chat tools never set it.
- `commit_draft` refuses if an item is removed or sold out at commit time. The team fixes the menu and previews again.

#### Menu lifecycle (confirmed behavior)

1. `create_event` copies the active event's **listed** menu at the active event's prices. Removed products are not copied. Temporary sold-out is not copied; the item starts available in the new event. If no event is active, the new menu starts empty.
2. Migration V3 backfills every existing event with every catalog product at the catalog price. Old events keep a full menu until items are removed from them.
3. `make seed-menu` applies `menu.csv` to the **Active Event only**. Check the active event first. Re-seeding overwrites that event's prices with the CSV, re-enables rows present in the CSV, and never deletes products missing from the CSV.
4. Shared across events: product name, category, aliases, and the catalog default price. Per event: charged price, listing, and temporary sold-out.
5. `upsert_product` updates the catalog default price, but the charged price always comes from `event_products`. The default only matters when a catalog product is attached to an event that never had it.
6. `products.is_available` is a global operations kill checked by every menu query. No chat tool sets it, and `set_product_enabled` cannot override it; clearing it requires SQL.

#### Menu operations

- Different menu for a new event: create it, activate it, then add, remove, or reprice. It starts as a copy of the active menu.
- Seed from CSV: activate the target event first, then `make seed-menu`.
- Rename or re-alias: global, affects every event. Keep the canonical name and add aliases when possible.
- Hide from one event: `set_product_enabled(enabled=false)`. Temporary habis: `set_product_available(false)`.
- Catalog name clash on add: `upsert_product` returns `conflict` with the existing id. Ask, then call again with that `product_id` to attach the existing catalog product to the event.
- Hide everywhere in an emergency: SQL on `products.is_available`, never a chat command.

### Customers

- Name only. `customers.normalized_name` is unique.
- Normalization: lowercase, trim, collapse spaces, strip punctuation and honorifics (`bu`, `ibu`, `pak`, `bapak`, `mas`, `mbak`, `mba`, `kak`, `bang`, `om`, `tante`).
- `find_customer` returns exact and near matches. The bot asks before reusing an existing customer. If the owner says it is a different person, use a distinguishing display name, for example "Susi kantor".
- Customers are created at commit, not at draft creation, so expired drafts do not pollute the table.

### Orders

- Order code: `#BK-001`, sequential per Event. Internal IDs are integers.
- Status: `confirmed` or `cancelled`.
- One confirmed order per (event, customer), enforced by a partial unique index.
- Order Items store product, quantity, unit-price snapshot, and an optional note.
- Cancelled orders stay. They are excluded from Omzet and per-product totals, and listed separately.
- Paid tracking: `is_paid` plus `paid_at`. `mark_order_paid` is reversible and needs no confirmation.
- Editing a Paid Order resets it to Unpaid and logs `payment_reset`.
- Cancelling a Paid Order warns that a manual refund is needed. The paid amount is shown separately as "cancelled paid".

### Drafts

- Kinds: `create`, `edit`, `cancel`.
- Internal ID: `D-` plus six Crockford base32 characters. Confirmation code: a 4-digit numeric `short_code`, globally unique across drafts. Drafts expire after 30 minutes.
- Drafts survive bot restarts because they live in SQLite.
- `commit_draft` is idempotent: committing an already-committed draft returns its result; an expired draft returns an error.
- Create draft: customer, items, notes. Preview shows event, customer, canonical product names, quantity, unit price, subtotal, total, and `OK <short_code>`.
- Edit draft: full replacement of items and notes. Preview shows before and after with the new total, and warns if the order is Paid. Ends with `OK <short_code>`.
- Cancel draft: preview shows customer, event, items, total, paid state, and `OK <short_code>`.

## Data model

Full DDL lives in the code. Tables and key constraints:

- `bazaar_events(id TEXT PK, name TEXT, event_date TEXT, cutoff_at INTEGER NULL, is_active INTEGER, created_at INTEGER)`
  - partial unique index on `is_active = 1`, so only one active event exists.
- `products(id INTEGER PK, name TEXT UNIQUE, category TEXT NOT NULL, price INTEGER CHECK(price >= 0), is_available INTEGER DEFAULT 1, created_at INTEGER, updated_at INTEGER)`
  - the shared catalog. `price` is the default used when a product is added to an event without an explicit price.
- `event_products(event_id FK, product_id FK, price INTEGER CHECK(price >= 0), is_available INTEGER DEFAULT 1, created_at INTEGER, updated_at INTEGER, PRIMARY KEY(event_id, product_id))`
  - the event's menu and prices. `is_available = 0` means removed from that event, keeping history and price.
- `product_aliases(id INTEGER PK, product_id FK, alias TEXT, normalized_alias TEXT, UNIQUE(product_id, alias))`
- `event_sold_out(event_id FK, product_id FK, created_at INTEGER, PRIMARY KEY(event_id, product_id))`
- `customers(id INTEGER PK, display_name TEXT, normalized_name TEXT UNIQUE, created_at INTEGER)`
- `orders(id INTEGER PK, event_id FK, customer_id FK, order_number INTEGER, status TEXT CHECK(status IN ('confirmed','cancelled')), is_paid INTEGER DEFAULT 0, paid_at INTEGER NULL, notes TEXT NULL, created_at INTEGER, updated_at INTEGER, cancelled_at INTEGER NULL, UNIQUE(event_id, order_number))`
  - partial unique index on `(event_id, customer_id) WHERE status = 'confirmed'`.
- `order_items(id INTEGER PK, order_id FK, product_id FK, product_name TEXT, quantity INTEGER CHECK(quantity > 0), unit_price INTEGER CHECK(unit_price >= 0), note TEXT NULL)`
  - `product_name` and `unit_price` are snapshots; catalog and event price edits never rewrite them.
- `order_audit_logs(id INTEGER PK, order_id FK, action TEXT, details TEXT, created_at INTEGER)`
- `drafts(id TEXT PK, short_code TEXT UNIQUE, kind TEXT CHECK(kind IN ('create','edit','cancel')), event_id FK, order_id FK NULL, customer_id FK NULL, customer_name TEXT NULL, payload TEXT, total INTEGER NULL, status TEXT CHECK(status IN ('pending','committed','expired')), expires_at INTEGER, created_at INTEGER, committed_at INTEGER NULL)`

All `*_at` columns are Unix seconds (`int64`). `event_date` is a WIB calendar date (`YYYY-MM-DD`).

Schema versions: V1 initial, V2 draft `short_code`, V3 `event_products` with a backfill that copies every existing product into every existing event at the catalog price.

Additions to the original design:

- `drafts`: two-phase writes must survive restarts.
- `event_sold_out`: temporary sold-out is per event.
- `event_products`: each event has its own menu and prices.
- `order_items.product_name`: keep the name sold even if the catalog is renamed.

No message IDs or JIDs are stored. Duplicate delivery is handled by draft idempotency plus the 30-minute expiry.

## MCP tools

All tools return `{ok: true, ...}` or `{ok: false, code, message}`. When a `formatted` field is present, the LLM relays it verbatim and never recalculates.

| Tool | Inputs | Behavior |
| --- | --- | --- |
| `list_events` | none | All events with active flag, confirmed order count, Omzet. |
| `create_event` | `name`, `event_date` (`YYYY-MM-DD`), `cutoff_at?`, `code?` | Creates an inactive event and copies the active event's listed menu and prices. Default code `EVENT-<date>`. Rejects duplicate codes and invalid dates. |
| `set_active_event` | `event_id` | Activates one event and deactivates the others. |
| `get_menu` | `event_id?` | The event's listed products with category, price, availability, and aliases. Defaults to Active Event. |
| `find_product` | `search_term`, `event_id?` | Candidates from the event's menu with id, canonical name, price, availability, matched alias. Empty means not found. |
| `set_product_available` | `product_id`, `available`, `event_id?` | Toggles per-event sold-out. Returns the new state. |
| `upsert_product` | `event_id?`, `product_id?`, `name`, `category`, `price`, `aliases?`, `available?` | Adds a catalog product and lists it on one event, or updates the shared name, category, and aliases plus that event's price and listing. Name clash returns `conflict` with the existing id. Menu admin, organizer DM only. |
| `set_product_enabled` | `event_id?`, `product_id`, `enabled` | Lists or removes a product on one event's menu. Other events and order history stay. Enabling a catalog product not yet listed adds it at the catalog price. For temporary sold-out use `set_product_available`. |
| `find_customer` | `name` | Exact and near matches with display name and order count. |
| `create_order_draft` | `customer_name`, `customer_id?`, `items[]`, `event_id?`, `notes?` | Creates a pending create draft. Refuses when the customer already has a confirmed order (`existing_order`). Returns preview and `short_code`. |
| `create_edit_draft` | `order_id`, `items[]`, `notes?` | Full replacement. Refuses cancelled orders. Returns before/after preview and `short_code`. |
| `create_cancel_draft` | `order_id` | Returns cancel preview and `short_code`. Already cancelled returns the current state. |
| `commit_draft` | `draft_code` (4 digits) | Applies create, edit, or cancel in one transaction. Idempotent. Refuses sold-out items. Returns order code and receipt. |
| `get_order` | `order_ref` (`#BK-001`, `BK-001`, or internal id), `event_id?` | Full order detail. |
| `search_orders` | `keyword`, `event_id?` | Compact matches by customer, product, or order code. |
| `mark_order_paid` | `order_id`, `paid` | Sets or clears paid state. Reversible, no draft. |
| `get_summary` | `event_id?`, `customer?` | Per-customer orders, per-product quantities, Omzet, paid and unpaid totals, cancelled count. Returns formatted text plus structured numbers. |

`items[]` entries are `{product_id, quantity, note?}`. Prices always come from the database, never from the model.

## Conversation flows

### DM: organizer forwards a friend's order

1. The organizer forwards the order text to the bot DM, or types it.
2. The bot extracts the customer and items. If no customer is named, it asks "Atas nama siapa?".
3. The bot resolves products and customers. Ambiguity means ask.
4. `create_order_draft` returns the preview: event, customer, items, total, `OK 4821`.
5. The organizer replies `OK 4821`. `commit_draft` returns the receipt.
6. If the customer already has an order, the bot offers to append through `create_edit_draft`.

### Group: team member relays an order

1. A team member forwards or posts the order text in the team group. It must name the customer, for example "Bu Ratna pesan sate 2".
2. The organizer replies to that message mentioning the bot: "@ZeroClaw catat ini".
3. ZeroClaw does not pass the quoted message. With `passive_group_context`, the bot reads the forwarded text from recent group history. If several recent order messages are plausible, it asks which one.
4. The same draft and confirmation flow applies. Preview and receipt are visible to the group.
5. To confirm, reply to the bot's preview with `OK 4821`. If the bot ignores a reply (mention gating), include `@ZeroClaw`.
6. Send the mention immediately after the order. Older messages can fall out of the 50-message context window.

### Event creation and switching (simulation)

1. "@ZeroClaw buat event Test Bazar 12 November 2026" calls `create_event`. The bot reports code, date, and that the event is inactive.
2. "@ZeroClaw aktifkan event TEST-2026-11-12" calls `set_active_event`. The bot announces the switch.
3. Simulate orders and summaries. They are scoped to the test event.
4. Switch back to `BAZAR-2026-11-15` before real use.

### Edits, cancellation, payment, summary

- **Edit:** fetch the order, `create_edit_draft` with the full new item list, preview, `OK <code>`, `commit_draft`. A paid order warns and resets to unpaid.
- **Cancel:** `create_cancel_draft`, preview, `OK <code>`, `commit_draft`. Soft cancel only.
- **Paid:** "Bu Ratna sudah bayar" calls `mark_order_paid(order, true)`. Reply "✅ #BK-001 ditandai lunas". Reversible with `paid = false`.
- **Summary:** call `get_summary` and relay the `formatted` text. Never compute totals. Each customer block is a bold `*#BK-001 — Customer*` header, then `Rp... · Lunas/Belum`, then items, then a blank line.

### Menu admin (organizer DM only)

1. Menu admin always targets one event: the named event, otherwise the active event. The bot names the event in the preview.
2. "tambah lauk ayam mboksiah 20rb" creates a catalog product and lists it on that event. "ubah harga sate jadi 45rb" updates that event only. Renaming or re-aliasing changes the shared catalog, so warn that other events are affected.
3. The bot shows the intended change: event, name, category, price, aliases, and `Balas OK <4 digit>`.
4. After `OK <code>`, call `upsert_product` (add or update) or `set_product_enabled` (list/remove) once. These tools write immediately; the conversational preview plus explicit OK is the guard.
5. Price changes affect new drafts only. Existing orders keep their snapshot.
6. "hapus X" means `set_product_enabled(enabled=false)` for that event, never a delete. Temporary sold-out stays `set_product_available`.
7. A new event copies the active event's listed menu at its prices. The bot can edit that copy without touching the old event.
8. Menu admin is prompted as DM-only. MCP tools cannot verify the sender, so this is a UX guard, not a security boundary.

## WhatsApp and ZeroClaw configuration

- Channel: WhatsApp Web build, persistent `session_path` under `data/wa-session`.
- Groups: allowlist exactly two groups: `Pengurus KPKM Awet Muda` (`<group-jid>`) and `Sie bazzar` (`<group-jid>`). `group_policy = "allowlist"`, `mention_only = true`, `passive_group_context = true`, `max_history_messages = 50`.
- DM: `dm_policy = "all"`. The bot shares the PM-WA WhatsApp account (`<bot-number>`), so the channel drops PM-WA command prefixes before the agent (see PM-WA coexistence below).
- No sender allowlist inside the allowed groups: every member may operate the bot. No JID storage in SQLite.
- Model: OpenCode Go custom OpenAI-compatible provider (`custom.bazaar`), `deepseek-v4.1-flash` (DeepSeek V4.1 Flash 1017). Base URL per Go docs (`https://opencode.ai/inference/go/openai/v1`). API key from the environment or secret store, never in the repo.
- Provider entry: `[providers.models.custom.bazaar]` in `zeroclaw/config.toml` (schema v3) with `uri`, `model`, `native_tools = true`, and `extra_headers.x-opencode-session`. Native tools reduce the system prompt from ~72k to ~7k chars and expose 17 function schemas.
- Risk profile `bazaar`: `level = "full"`, `auto_approve = ["*"]`, and `allowed_tools` pinned to the 17 `bazaar__*` tools. This removes shell, filesystem, and every other built-in from the model.
- MCP: stdio server spawned by ZeroClaw. Command `/home/<user>/bazaar-minggu/bin/bazaar-mcp`, args `["serve", "--db", "/home/<user>/bazaar-minggu/data/orders.db"]`. Grant the server through `mcp_bundles`.
- Agent prompt: `prompts/system.md`, copied to `agent-workspace/IDENTITY.md`, encodes the Hard Rules, the confirmation protocol, ambiguity rules, and "relay formatted text, never calculate".

### PM-WA coexistence

The bot runs on the same WhatsApp account as PM-WA (`<bot-number>`). Both bridges receive the same DMs; the ZeroClaw channel drops PM-WA commands before the agent runs.

Filtered first-line prefixes: `add-list`, `add-task`, `add-tasks`, `publish-list`, `stop-list`, `delete-task`, `delete-list`, `list-lists`, `list-tasks`, `tasklist-web`, and `done:` / `todo:` prefixes.

The filter lives in the ZeroClaw checkout at `~/.zeroclaw/src/crates/zeroclaw-channels/src/whatsapp_web.rs` (`is_pmwa_command`), not in this repo. Re-apply it after every ZeroClaw update. If another command bot shares the account, replace the hardcoded list with a config field before that update.

`zeroclaw channel doctor` may report `WhatsApp unhealthy` because it checks the unused Cloud API channel; the runtime trace's `state: connected` is authoritative.

Known adapter limits in ZeroClaw v0.9.0:

- Quoted message text and quoted sender are not passed to the agent. Passive context is the workaround.
- The agent cannot quote on send. Confirmations use IDs.
- Sender, chat, and message IDs reach the LLM through a `[turn-context]` prefix, but MCP tools receive only model-supplied arguments. Identity checks are not a security boundary.
- Text only. Images and voice notes are ignored.

## Deployment

- Host: `<user>@<vps-host>`, the same VPS as PM-WA, with a separate checkout and session.
- Checkout: `/home/<user>/bazaar-minggu`.
- tmux session: `bazaar` (new; never touch `pm-wa`).
- Timezone: `Asia/Jakarta`.

Host baseline (measured 2026-11-12): 1.9 GiB RAM total, 2 vCPU, 6 GB swap (4 GB + 2 GB) added for the ZeroClaw source build. Go 1.25.9, SQLite 3.46.1, AlmaLinux 10. PM-WA uses ~47 MB RSS; total used ~700 MiB. A Go MCP server (~10–20 MB) is well within budget.

Steps:

1. SSH to the host and clone or pull the repo into the checkout.
2. Build the MCP server: `make build` (`CGO_ENABLED=1 go build -o bin/bazaar-mcp ./cmd/bazaar-mcp`).
3. Install ZeroClaw from source with the `whatsapp-web` feature. The prebuilt v0.8.7 tarball and .deb do **not** include WhatsApp Web (`zeroclaw channel list` says "configured, not compiled"). See the build note below.
4. Write the ZeroClaw config: provider, MCP server, WhatsApp policies. Keep the Go API key in the secret store or environment.
5. Seed the database: `make init` and `make seed-menu MENU=menu.csv`.
6. Start a new tmux session named `bazaar`. Run ZeroClaw in the foreground with logs at `logs/zeroclaw.log`.
7. Pair WhatsApp by scanning the QR from the bot account. Confirm the session path persists.
8. Dry-run in a test event, then switch to `BAZAR-2026-11-15`.

### ZeroClaw WhatsApp Web build

- The prebuilt release binary does **not** include WhatsApp Web. `zeroclaw channel list` reports "WhatsApp (configured, not compiled)".
- Build on the VPS with fast codegen:
  ```
  env CARGO_BUILD_JOBS=1 \
      CARGO_PROFILE_RELEASE_OPT_LEVEL=0 \
      CARGO_PROFILE_RELEASE_CODEGEN_UNITS=16 \
      CARGO_PROFILE_RELEASE_LTO=false \
      ~/downloads/install-zeroclaw.sh --source --minimal \
      --features agent-runtime,gateway,whatsapp-web \
      --apps none --skip-quickstart --no-modify-path
  ```
- Answer `y` if the `--minimal` warning prompts.
- `--minimal` plus the three features drops the extra channel bundles and ACP/Prometheus. `gateway` still pulls `schema-export`, so the release profile's `opt-level=z`, `codegen-units=1`, and LTO are what make `zeroclaw-config` thrash; the env overrides above compile it fast. The binary is unoptimized but fine for this low-volume bot.
- Requires `protoc`, `cmake`, `clang`, and swap (6 GB total).
- The script installs the binary at `~/.cargo/bin/zeroclaw`. `start-bazaar.sh` prefers that path over the prebuilt `~/bin/zeroclaw`.
- Source checkout: `~/.zeroclaw/src`; build log: `~/downloads/zeroclaw-build-fast.log`.
- Verify with `~/.cargo/bin/zeroclaw --config-dir ~/bazaar-minggu/zeroclaw channel list`; WhatsApp Web must be compiled, not "configured, not compiled".
- The config file must start with `schema_version = 3`. A v1-shaped file silently ignores `providers` and `channels`; if migrating, verify with `config list --filter providers.models.custom.bazaar` and `--filter channels.whatsapp.bazaar` because the migrator can over-nest aliases.
- Incremental rebuild after a channel-source patch: run `cargo install` directly with the same fast profile env from `~/.zeroclaw/src`:
  ```
  env CARGO_BUILD_JOBS=1 \
      CARGO_PROFILE_RELEASE_OPT_LEVEL=0 \
      CARGO_PROFILE_RELEASE_CODEGEN_UNITS=16 \
      CARGO_PROFILE_RELEASE_LTO=false \
      ~/.cargo/bin/cargo install --path . --locked --force \
      --no-default-features --features agent-runtime,gateway,whatsapp-web
  ```
  The 2026-11-12 PM-WA filter rebuild took 14m45s because it recompiled the WhatsApp protobuf/signal stack. Log: `~/downloads/zcfilter.log`.

### First start

Restrict the config first: `chmod 600 ~/bazaar-minggu/zeroclaw/config.toml`.

1. Set the OpenCode Go key with the masked prompt:
   `~/.cargo/bin/zeroclaw --config-dir ~/bazaar-minggu/zeroclaw config set providers.models.custom.bazaar.api_key`
   Before the source build finishes, the same command works with the existing `~/bin/zeroclaw`.
2. Attach to the tmux session: `tmux a -t bazaar`.
3. Start: `./start-bazaar.sh`.
4. Scan the WhatsApp QR from the bot account. The session path is `data/wa-session.db`; never delete it.
5. Test a DM to the bot, then a mention in the team group.

Initial WhatsApp policy: the dry-run config uses `dm_policy = "all"`, `group_policy = "all"`, and `peer_groups.bazaar.external_peers = ["*"]`. After the team group JID and organizer JID are known, set `allowed_groups = ["<group>@g.us"]` and replace `["*"]` with the organizer's number, then restart the daemon.

Host warnings, carried over from PM-WA's AGENTS.md:

- **Never run `tmux capture-pane` on this host.** The previous host note reported a crash on tmux 3.3a-13; the host now reports tmux next-3.4, but do not test it on production.
- Verify with `pgrep -af zeroclaw`, log files, and WhatsApp test messages. Keep remote steps small and check between them.
- Do not stop or restart `pm-wa`. This bot uses its own session and checkout.
- Check memory with `free -h` before running two WhatsApp bots. If memory is tight, reduce this bot's test load, never PM-WA's.

Backup:

- Before the bazaar and after each day: `sqlite3 data/orders.db ".backup 'backups/orders-YYYY-MM-DD-HHMM.db'"`.
- Backups are the only recovery path. There are no hard deletes.

## Build and verification

- `go version` must be 1.25 or newer (the VPS has 1.25.9).
- `make build` runs `CGO_ENABLED=1 go build -o bin/bazaar-mcp ./cmd/bazaar-mcp`.
- `make lint` runs `go vet ./...`.
- `make test` runs `go test ./...`.
- `make init` creates the schema and seeds `BAZAR-2026-11-15`.
- `make seed-menu MENU=menu.csv` seeds products and aliases into the Active Event.
- `make run` runs `BAZAAR_DB=data/orders.db ./bin/bazaar-mcp serve`.

Before finishing any change: lint, unit tests, and integration tests. Report the actual result of each check. Manual WhatsApp checks must be labeled manual, never implied to pass.

## Testing plan

Unit tests in external `_test` packages (table-driven where there are multiple cases):

- Rupiah formatting and integer totals, no floats.
- Customer normalization and honorific stripping: "Bu Susi" matches "Susi"; distinct-person path.
- Product name and alias matching; ambiguous candidates; not found.
- Draft expiry, idempotent commit, expired commit.
- One confirmed order per (event, customer); a cancelled order allows a new one.
- Edit full replacement, paid-to-unpaid reset, total recalculation.
- Cancel soft delete, excluded from Omzet, cancelled-paid separate.
- Paid and unpaid totals; summary per customer and per product.
- Event activation, only one active; draft event isolation.
- Per-event menu: a product added to one event is absent from another; the same product can have different prices; new events copy the active event's listed menu; removing from one event leaves the other and order history intact.
- Per-event sold-out; commit refuses sold-out; other event unaffected.
- Price snapshot at draft creation; later price changes, in the same or another event, do not alter confirmed orders.

Integration tests (temp SQLite via `t.TempDir()`):

- Temp SQLite: seed event and menu, create draft, commit, edit, pay, cancel, summarize. Assert exact strings and numbers.
- MCP stdio smoke test: `tools/list` includes every tool; one create-and-commit round trip.

Manual dry run in a test event and the team group:

- Forward a text order, mention the bot, confirm, edit, mark paid, cancel, summarize.
- Verify passive context picks the right message.
- Verify whether a reply to the bot needs an explicit mention.
- Verify images and voice notes are ignored and the bot asks for text.
- Switch back to the real event and confirm previews name the right event.

## Non-goals for v1

Web UI or dashboard, payment gateway or QRIS, customer self-service, simultaneous multi-tenant events, cutoff enforcement, image or voice parsing, JID or identity authorization, automatic reminders or broadcasts, hard deletes, price editing through chat, multiple separate orders per customer, and patching ZeroClaw to pass quoted replies.

## Open items

- Dry-run the order flow in both allowed groups and confirm the PM-WA command filter.
- Confirm PM-WA reminder `yes` / `no` replies do not cause duplicate ZeroClaw replies; add them to the filter only if they do.
- Re-apply the PM-WA prefix patch after every ZeroClaw update.
- Monitor provider latency: the first live turn took ~20s, and the reply-intent precheck timed out at 5s (it fails open).
- Later: decide whether to patch ZeroClaw upstream to pass quoted text for the exact reply UX.

## Risks

- WhatsApp Web is unofficial. A session drop or ban takes the bot down. Prefer a dedicated bot number.
- Passive context is probabilistic. A mention far from the order, or several recent orders, can pick the wrong one. Preview plus confirmation is the guard; keep the mention immediate and name the customer.
- Confirmation is not a security boundary. The group is trusted. Prompt injection could skip steps. Do not use this for untrusted groups.
- The Go plan terms describe coding-agent traffic. If it is throttled, switch the provider to a pay-as-you-go key.
- Two WhatsApp bots on one VPS: watch memory and CPU, and never touch `pm-wa`.
- Name-only customers can collide. Always confirm near matches.
- No backup means data loss. Back up the database.
