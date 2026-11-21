# Architecture

Status: 2026-11-21. Matches ADR 0001 to 0004 after the decision to keep ZeroClaw.

## Level 1: system context

ZeroClaw holds the channel sessions, so it is the only component that knows who
spoke. It passes that on with every tool call, and the backend refuses anything
that arrives without it.

```mermaid
flowchart LR
    subgraph people["People"]
        ORG["Organizer"]
        SM["Store member"]
    end

    subgraph ch["Chat channels"]
        WA["WhatsApp Web"]
        TG["Telegram (later)"]
    end

    ZC["ZeroClaw<br/>mention gate, passive context,<br/>LLM, injects identity into _meta"]
    BE["bazaar-mcp<br/>resolves scope, role matrix,<br/>WHERE store_id = ?"]
    DB[("SQLite<br/>orders.db")]

    ORG --> WA
    SM --> WA
    SM -.-> TG
    WA --> ZC
    TG -.-> ZC
    ZC -->|"tools/call + context"| BE
    BE --> DB

    classDef anchor stroke-width:3px,stroke:#b00
    class ZC anchor
```

The thick box is the trust anchor: the local parent process that spawns
`bazaar-mcp` over stdio. No network hop, no token.

## Level 2: components

```mermaid
flowchart TB
    subgraph zc["ZeroClaw"]
        direction TB
        ADW["whatsapp_web channel"]
        ADT["telegram channel"]
        GATE["mention gate, allowlist,<br/>PM-WA prefix filter"]
        ORCH["orchestrator turn<br/>reply_target, sender, message_id"]
        LLM["LLM"]
        DISP["tool dispatch<br/>injects identity into _meta"]
        ADW --> GATE
        ADT --> GATE
        GATE --> ORCH
        ORCH --> LLM
        LLM --> DISP
    end

    subgraph be["bazaar-mcp"]
        RES["scope resolver<br/>chat or sender to store + role"]
        ROL["role matrix"]
        HND["tool handlers"]
        QRY["db queries<br/>WHERE store_id = ?"]
        RES --> ROL --> HND --> QRY
    end

    DB[("SQLite")]

    ORCH --> DISP
    DISP --> RES
    QRY --> DB
    RES --> DB
```

## Level 3: where the pieces live

| Concern | Package | Notes |
|---|---|---|
| Channel sessions, mention gating, LLM | ZeroClaw (Rust, forked) | fork carries two patches: PM-WA filter, context injection |
| Tool-call context | `zeroclaw-tools` (`mcp_tool.rs`, `mcp_client.rs`) | `_meta` attached to the request |
| Scope resolution, role matrix | `mcpserver/` | one wrapper around `mcp.AddTool` |
| Policy, money, drafts | `orders/` | receives a `Scope` |
| SQL, store predicates | `db/` | every statement takes a store |
| User-visible text | `format/` | never receives a channel identity |

Test scenarios that drive this system over WhatsApp Web against the production
deployment: [`docs/testing/e2e-whatsapp.md`](testing/e2e-whatsapp.md).

## Request path

```mermaid
sequenceDiagram
    autonumber
    actor M as Store member
    participant WA as WhatsApp
    participant ZC as ZeroClaw
    participant BE as bazaar-mcp
    participant DB as SQLite

    M->>WA: catat Bu Ratna sate 2
    WA->>ZC: message with chat_ref and sender_ref
    ZC->>ZC: mention gate, then LLM picks create_order_draft
    ZC->>BE: tools/call with identity in _meta
    BE->>DB: store_groups by (channel, chat_ref)
    DB-->>BE: store 1, role staff
    BE->>DB: INSERT draft WHERE store_id = 1
    DB-->>BE: draft and 4-digit code
    BE-->>ZC: ok, formatted preview
    ZC-->>WA: preview + OK 4821
    WA-->>M: preview + OK 4821
```
