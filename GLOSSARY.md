# Bazar KPKM Order Assistant

The domain of a WhatsApp order assistant for the Bazar KPKM team: bazaar events, a menu, customers, confirmed orders, payments, and summaries.

## Events

**Event**:
One bazaar occurrence with a date and an optional order cutoff. Orders never mix between events.
_Avoid_: Bazaar (as a data term), session

**Active Event**:
The event that new orders default to when the message does not name one. Exactly one event is active at a time.
_Avoid_: Current bazaar, selected event

**Test Event**:
An event created to rehearse the order flow. It has its own orders and never affects a real event's summary.
_Avoid_: Dummy event, sandbox

**Cutoff**:
The date and time after which orders would normally close. It is informational only; the assistant never rejects an order because of it.
_Avoid_: Deadline, closing time

## Menu

**Menu**:
The set of products and prices offered for one event. Each event has its own menu and prices; a shared product catalog supplies names, categories, and aliases.
_Avoid_: Global menu, one price list for all events

**Category**:
A menu grouping — Makanan, Cemilan, Minuman — used for display.
_Avoid_: Section, group

**Product**:
A sellable catalog item with one canonical name, a category, and a default integer Rupiah price. The price actually charged comes from the event's menu.
_Avoid_: Item (use Order Item), SKU, dish

**Flavor**:
A choice within a drink line, such as Gurin, Orenji, Original, or Honey Lime.
_Avoid_: Variant, type

**Alias**:
An alternative name, abbreviation, or common typo that points to a product. Aliases are suggestions; an ambiguous alias is confirmed with the user.
_Avoid_: Synonym, nickname

**Sold Out**:
A product is unavailable for one event. Sold-out is per event, not global.
_Avoid_: Unavailable (global), out of stock

## People and roles

**Customer**:
The person an order is for, identified by name only. Not the team member who relays the order, and not a WhatsApp account.
_Avoid_: Client, buyer, user, account

**Team Member**:
A person in the bazaar team group who relays orders and operates the assistant. A team member is not a customer unless the food is for them.
_Avoid_: Admin, operator, user

**Relayed Order**:
An order message that a team member forwards or posts on a customer's behalf. It must name the customer.
_Avoid_: Forwarded message (the WhatsApp artifact)

**Customer Name Normalization**:
The rule that lowercases a name and strips punctuation and honorifics such as Bu, Pak, Mas, Mbak, and Kak, so "Bu Susi" and "Susi" can be compared. A match is confirmed, never assumed.
_Avoid_: Fuzzy match, dedupe

## Orders

**Order**:
One customer's confirmed set of order items for one event, with a code, a status, and a paid state.
_Avoid_: Pesanan (in code and docs), transaction, sale

**Order Item**:
One product, its quantity, its snapshot unit price, and an optional note within an order.
_Avoid_: Line item, product row

**Order Code**:
The human-readable per-event identifier displayed as #BK-001.
_Avoid_: Order ID (internal), receipt number

**Confirmed**:
An order that has been committed and counts toward Omzet.
_Avoid_: Saved, finalized

**Cancelled**:
An order that was committed and then cancelled. It stays for audit and is excluded from Omzet.
_Avoid_: Deleted, removed

**Draft**:
A proposed create, edit, or cancel shown before it is applied. It expires and is applied only by an explicit confirmation.
_Avoid_: Preview, pending order

**Confirmation**:
The explicit `CONFIRM <draft id>` reply that authorizes committing a draft. A plain "yes" is not confirmation.
_Avoid_: Approval, OK

**Edit**:
A draft that changes an order's items or notes after confirmation. Editing a paid order resets it to unpaid.
_Avoid_: Modification, update

**Cancellation**:
The soft state change that marks an order cancelled while keeping the order and its history.
_Avoid_: Delete, removal

## Money and reporting

**Paid / Unpaid**:
Whether the team has received cash for a confirmed order. Payment is separate from order status; a cancelled order can still have been paid, which needs a manual refund.
_Avoid_: Settled, completed

**Omzet**:
The total value of confirmed, non-cancelled orders for an event, regardless of payment. It is not cash collected.
_Avoid_: Revenue, income, sales

**Paid Total / Unpaid Total**:
The part of Omzet marked paid, or not marked paid.
_Avoid_: Cash, balance

**Summary**:
A report from the order records with per-customer orders, per-product quantities, Omzet, paid and unpaid totals, and the cancelled count.
_Avoid_: Report, recap

**Audit Log**:
The append-only record of order actions: created, edited, cancelled, paid, unpaid. It stores no WhatsApp identities.
_Avoid_: History, changelog
