# Architecture

Status: draft, 2026-11-20. The diagrams match ADR 0001 to 0004.

## Level 1: system context

Who talks to what, and where the trust boundary sits. The gateway is the only
component allowed to say who is calling.

```mermaid
flowchart LR
    subgraph people["People"]
        ORG["Organizer"]
        SM["Store member"]
    end

    subgraph ch["Chat channels"]
        WA["WhatsApp"]
        TG["Telegram"]
    end

    GW["Gateway<br/>resolves chat and sender,<br/>issues signed scope"]
    AG["Agent runtime<br/>LLM chooses tools"]
    BE["bazaar-mcp<br/>17 tools, policy, money"]
    DB[("SQLite<br/>orders.db")]

    ORG --> WA
    SM --> WA
    SM --> TG
    WA --> GW
    TG --> GW
    GW -->|"text + signed scope"| AG
    AG -->|"tools/call, scope in _meta"| BE
    BE --> DB

    classDef boundary stroke-width:3px,stroke:#b00
    class GW boundary
```

## Level 2: components

Inside the three components. Two channels today, and a new one is an adapter plus
nothing else.

```mermaid
flowchart TB
    subgraph gw["Gateway"]
        direction TB
        ADW["whatsapp adapter"]
        ADT["telegram adapter"]
        RES["resolver<br/>chat_ref, sender_ref to store + role"]
        SIGN["signer<br/>HMAC-SHA256, 300s"]
        ADW --> RES
        ADT --> RES
        RES --> SIGN
    end

    subgraph ag["Agent runtime"]
        LLM["LLM"]
        TC["tool call assembly"]
        LLM --> TC
    end

    subgraph be["bazaar-mcp"]
        VER["scope verifier"]
        ROL["role matrix"]
        HND["tool handlers"]
        QRY["db queries<br/>WHERE store_id = ?"]
        VER --> ROL --> HND --> QRY
    end

    DB[("SQLite")]

    SIGN --> LLM
    TC --> VER
    QRY --> DB
    ROL -->|"live authority row"| DB
```

## Level 3: where the pieces live

| Concern | Package | Notes |
|---|---|---|
| Channel authentication, scope signing | `gateway/` (new) | only writer of scope tokens |
| Scope verification, role matrix | `mcpserver/` | one wrapper around `mcp.AddTool` |
| Policy, money, drafts | `orders/` | unchanged, receives `Scope` |
| SQL, store predicates | `db/` | every statement takes a store |
| User-visible text | `format/` | never receives a channel identity |

Test scenarios that drive this system over WhatsApp Web:
[`docs/testing/e2e-whatsapp.md`](testing/e2e-whatsapp.md).

## Request path

```mermaid
sequenceDiagram
    autonumber
    actor M as Store member
    participant WA as WhatsApp
    participant GW as Gateway
    participant AG as Agent runtime
    participant BE as bazaar-mcp
    participant DB as SQLite

    M->>WA: catat Bu Ratna sate 2
    WA->>GW: chat_ref, sender_ref, text
    GW->>DB: lookup binding or membership
    DB-->>GW: store 1, role staff, account 12
    GW->>GW: sign scope, exp = now + 300
    GW->>AG: text + scope token
    AG->>BE: tools/call create_order_draft + _meta scope
    BE->>BE: verify signature, iss, aud, exp
    BE->>DB: live authority row, store 1
    DB-->>BE: status active, role staff
    BE->>DB: INSERT draft ... WHERE store_id = 1
    DB-->>BE: draft D-7K2M9Q, code 4821
    BE-->>AG: ok, formatted preview
    AG-->>WA: preview + "OK 4821"
    WA-->>M: preview + "OK 4821"
```
