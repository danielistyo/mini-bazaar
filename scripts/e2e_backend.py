#!/usr/bin/env python3
"""Run the backend e2e scenarios against a copy of production, over MCP stdio.

Every call carries _meta["id.bazaar/context"], exactly as the patched runtime
sends it. WhatsApp-only behaviour (mentions, media, passive context) is not here.
"""
import json
import shutil
import sqlite3
import subprocess
import sys
import time

import os

SRC = os.environ.get("BAZAAR_DB", "data/orders.db")
DB = os.environ.get("BAZAAR_E2E_DB", "/tmp/e2e.db")
BIN = os.environ.get("BAZAAR_BIN", "bin/bazaar-mcp")

fails = []
passes = []


def check(name, cond, detail=""):
    if cond:
        passes.append(name)
        print(f"PASS {name}")
    else:
        fails.append(name)
        print(f"FAIL {name} {detail}")


if not os.path.exists(SRC):
    print(f"FAIL source database not found: {SRC} (set BAZAAR_DB)", file=sys.stderr)
    sys.exit(2)
if not os.path.exists(BIN):
    print(f"FAIL binary not found: {BIN} (run make build, or set BAZAAR_BIN)", file=sys.stderr)
    sys.exit(2)

for suffix in ("", "-wal", "-shm"):
    try:
        os.remove(DB + suffix)
    except FileNotFoundError:
        pass
con = sqlite3.connect(SRC)
con.execute(f"VACUUM INTO '{DB}'")
con.close()

db = sqlite3.connect(DB)
group_kpkm = db.execute(
    "SELECT chat_ref FROM store_groups WHERE role='staff' AND status='active' LIMIT 1"
).fetchone()[0]
group_org = db.execute(
    "SELECT chat_ref FROM store_groups WHERE role='organizer' AND status='active' LIMIT 1"
).fetchone()[0]
rud = db.execute(
    """SELECT ac.channel, ac.ref FROM account_channels ac
         JOIN store_members m ON m.account_id = ac.account_id
        WHERE m.store_id = 2 AND m.status='active' LIMIT 1"""
).fetchone()
susan_event = db.execute(
    "SELECT id FROM bazaar_events WHERE store_id=2 LIMIT 1"
).fetchone()[0]
kpkm_event = db.execute(
    "SELECT id FROM bazaar_events WHERE store_id=1 AND is_active=1 LIMIT 1"
).fetchone()[0]
scenario = db.execute(
    "SELECT COUNT(*) FROM orders WHERE event_id=?", (kpkm_event,)
).fetchone()[0]
db.close()

proc = subprocess.Popen(
    [BIN, "serve", "--db", DB],
    stdin=subprocess.PIPE,
    stdout=subprocess.PIPE,
    stderr=subprocess.DEVNULL,
    text=True,
    bufsize=1,
)
next_id = [0]


def send(method, params):
    next_id[0] += 1
    msg = {"jsonrpc": "2.0", "id": next_id[0], "method": method, "params": params}
    proc.stdin.write(json.dumps(msg) + "\n")
    proc.stdin.flush()
    while True:
        line = proc.stdout.readline()
        if not line:
            raise RuntimeError("server closed")
        try:
            obj = json.loads(line)
        except json.JSONDecodeError:
            continue
        if obj.get("id") == next_id[0]:
            return obj


def ctx(channel="whatsapp", chat="", sender="", include=True):
    params = {}
    if include:
        params["_meta"] = {
            "id.bazaar/context": {
                "channel": channel,
                "chat_ref": chat,
                "sender_ref": sender,
            }
        }
    return params


def call(tool, args, channel="whatsapp", chat="", sender="", include=True):
    params = ctx(channel, chat, sender, include)
    params["name"] = tool
    params["arguments"] = args
    res = send("tools/call", params)
    out = res.get("result", {}).get("structuredContent") or {}
    if not out:
        content = res.get("result", {}).get("content") or []
        if content:
            try:
                out = json.loads(content[0].get("text", "{}"))
            except json.JSONDecodeError:
                out = {"raw": content[0].get("text")}
    if not out and "error" in res:
        out = {"ok": False, "code": "rpc_error", "message": json.dumps(res["error"])}
    return out


send("initialize", {
    "protocolVersion": "2024-11-05",
    "capabilities": {},
    "clientInfo": {"name": "e2e", "version": "1"},
})

# E2E-A5: unbound chat is refused
r = call("get_menu", {"event_id": kpkm_event}, chat="999999@g.us", sender="628999@s.whatsapp.net")
check("A5 unbound chat -> no_store", r.get("ok") is False and r.get("code") == "no_store", r)

# E2E-0001: no context at all
r = call("get_menu", {"event_id": kpkm_event}, include=False)
check("0001 missing context -> no_store", r.get("ok") is False and r.get("code") == "no_store", r)

# happy path in the staff group
r = call("get_menu", {}, chat=group_kpkm, sender="628111@s.whatsapp.net")
check("O1 staff reads menu", r.get("ok") is True and len(r.get("data", {}).get("products", [])) > 0, r)
menu = r.get("data", {}).get("products", [])
staff_product = menu[0]["id"] if menu else 0
staff_price = menu[0]["price"] if menu else 0

# E2E-A7: staff cannot touch the menu
r = call("upsert_product", {"name": "E2E Staff", "category": "Makanan", "price": 1000},
         chat=group_kpkm, sender="628111@s.whatsapp.net")
check("A7 staff -> role_denied", r.get("ok") is False and r.get("code") == "role_denied", r)

# E2E-A8: organizer can
r = call("upsert_product", {"name": "E2E Uji", "category": "Makanan", "price": 2000},
         chat=group_org, sender="628222@s.whatsapp.net")
check("A8 organizer adds product", r.get("ok") is True, r)

# E2E-O17 / A10: another store's event is not reachable
r = call("get_menu", {"event_id": susan_event}, chat=group_kpkm, sender="628111@s.whatsapp.net")
check("O17 foreign event -> not_found", r.get("ok") is False and r.get("code") == "not_found", r)

# DM resolves through the person's membership (store 2)
r = call("get_menu", {}, channel=rud[0], chat=rud[1], sender=rud[1])
check("DM resolves to its own store", r.get("ok") is False and r.get("code") == "no_active_event", r)
r = call("get_menu", {"event_id": kpkm_event}, channel=rud[0], chat=rud[1], sender=rud[1])
check("DM cannot read the other store's event", r.get("ok") is False and r.get("code") == "not_found", r)

# E2E-O1 + O2 + O3: draft, wrong code, then the real code
r = call("create_order_draft", {"customer_name": "Uji E2E", "items": [{"product_id": staff_product, "quantity": 2}]},
         chat=group_kpkm, sender="628111@s.whatsapp.net")
check("O1 draft created", r.get("ok") is True, r)
code = r.get("data", {}).get("short_code") or r.get("data", {}).get("draft", {}).get("short_code")
r2 = call("commit_draft", {"draft_code": "0000"}, chat=group_kpkm, sender="628111@s.whatsapp.net")
check("O3 wrong code refused", r2.get("ok") is False, r2)
r3 = call("commit_draft", {"draft_code": code}, chat=group_kpkm, sender="628111@s.whatsapp.net")
check("O1 commit succeeds", r3.get("ok") is True, r3)
order_id = (r3.get("data") or {}).get("order", {}).get("id")

# E2E-O5: idempotent commit
r4 = call("commit_draft", {"draft_code": code}, chat=group_kpkm, sender="628111@s.whatsapp.net")
check("O5 second commit is idempotent", r4.get("ok") is True, r4)

# E2E-P1 / P2: payment
if order_id:
    r = call("mark_order_paid", {"order_id": order_id, "paid": True}, chat=group_kpkm, sender="628111@s.whatsapp.net")
    check("P1 mark paid", r.get("ok") is True, r)
    r = call("mark_order_paid", {"order_id": order_id, "paid": False}, chat=group_kpkm, sender="628111@s.whatsapp.net")
    check("P2 mark unpaid", r.get("ok") is True, r)

# E2E-O11 / O12: cancel, then cancel again
r = call("create_cancel_draft", {"order_id": order_id}, chat=group_kpkm, sender="628111@s.whatsapp.net")
check("O11 cancel draft", r.get("ok") is True, r)
ccode = (r.get("data") or {}).get("short_code") or (r.get("data") or {}).get("draft", {}).get("short_code")
r = call("commit_draft", {"draft_code": ccode}, chat=group_kpkm, sender="628111@s.whatsapp.net")
check("O11 cancel commits", r.get("ok") is True, r)
r = call("create_cancel_draft", {"order_id": order_id}, chat=group_kpkm, sender="628111@s.whatsapp.net")
check("O12 second cancel returns state", r.get("ok") in (True, False), r)

# E2E-S1: summary still adds up
r = call("get_summary", {}, chat=group_kpkm, sender="628111@s.whatsapp.net")
data = r.get("data") or {}
ok = r.get("ok") is True and data.get("omzet", 0) == data.get("paid_total", 0) + data.get("unpaid_total", 0)
check("S5 omzet = paid + unpaid", bool(ok), data)

# E2E-A3 / A4: claim issued by organizer, redeemed in a new chat, used there
r = call("create_group_claim", {"role": "staff"}, chat=group_org, sender="628222@s.whatsapp.net")
check("A3 claim issued", r.get("ok") is True, r)
claim = (r.get("data") or {}).get("code")
new_chat = "120363000000000001@g.us"
r = call("redeem_group_claim", {"code": "ZZZZZZ"}, chat=new_chat, sender="628333@s.whatsapp.net")
check("A4 wrong code refused", r.get("ok") is False, r)
r = call("redeem_group_claim", {"code": claim}, chat=new_chat, sender="628333@s.whatsapp.net")
check("A3 redeem binds chat", r.get("ok") is True, r)
r = call("get_menu", {}, chat=new_chat, sender="628333@s.whatsapp.net")
check("A3 bound chat acts as staff", r.get("ok") is True, r)
r = call("upsert_product", {"name": "E2E Claimed", "category": "Makanan", "price": 1000},
         chat=new_chat, sender="628333@s.whatsapp.net")
check("A7 claimed staff cannot manage menu", r.get("ok") is False and r.get("code") == "role_denied", r)

# E2E-A4 again: the same code cannot be reused
r = call("redeem_group_claim", {"code": claim}, chat="120363000000000002@g.us", sender="628333@s.whatsapp.net")
check("A4 reused code refused", r.get("ok") is False, r)

# E2E-A2: one store per person
r = call("create_store", {"code": "DUA", "name": "Kedua"}, channel=rud[0], chat=rud[1], sender=rud[1])
check("A2 second store refused", r.get("ok") is False and r.get("code") == "conflict", r)

proc.stdin.close()
proc.wait(timeout=10)

print(f"\n{len(passes)} passed, {len(fails)} failed")
if fails:
    print("failed: " + ", ".join(fails))
sys.exit(1 if fails else 0)
