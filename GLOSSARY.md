# Bazar KPKM Order Assistant

The domain of a chat assistant that lets several food businesses record orders taken in bazaar groups: stores, events, a menu, customers, confirmed orders, payments, and summaries. It began as one WhatsApp bot for one team; it now serves many stores over more than one channel.

## Stores

**Store**:
One business that sells food, with its own events, menu, and customers, operable from several channels. Nothing in one store is visible to another.
_Avoid_: Tenant, account, organisation, team, seller

**Store Code**:
The short uppercase identifier a store is addressed by, such as `KPKM`. It never changes once the store exists.
_Avoid_: Slug, handle, username, store id

**Suspended Store**:
A store that has been switched off. Its data and history remain, but every request from its members is refused until it is switched back on.
_Avoid_: Deleted store, banned store, archived store

## Access

**Channel**:
The app a message arrives on, such as WhatsApp or Telegram. A store can be operated from more than one channel, and the data is the same on all of them.
_Avoid_: Platform, provider, integration

**Account**:
One human, the same person on every channel they use. An account belongs to one store at a time.
_Avoid_: User, login, profile, member

**Channel Identity**:
How one account or one group appears on one channel: a WhatsApp number, a Telegram user id. A human has one per channel, and they count as the same person only once that link has been proven.
_Avoid_: JID, phone number, external id, handle

**Store Member**:
An account that may operate a store, with the role given to it there. Most people who touch a store are never members.
_Avoid_: Team member, admin, operator, user

**Group Binding**:
A chat group registered to a store, so that everyone in the group acts with the role written on the binding. Its members need no account of their own.
_Avoid_: Group access, chat registration, group owner

**Role**:
How much of a store someone may operate. Organizers manage the store itself; staff take and settle orders.
_Avoid_: Permission, access level, privilege

**Organizer**:
The role allowed to change the store: its menu, its events, and who else has access.
_Avoid_: Owner, admin, superuser

**Staff**:
The role allowed to record, edit, cancel, and settle orders, but not to change the menu, the events, or access.
_Avoid_: Member, operator, user

**Claim Code**:
A short one-time code that moves trust from one place to another: from an organizer's direct message into a group, or from one channel to another. Whoever can read the message holds the code.
_Avoid_: Invite code, token, OTP, password

**Scope**:
The store, role, and caller that a single incoming message acts under. It is settled before the message reaches the assistant, and it bounds everything the assistant may do in that reply.
_Avoid_: Context, session, tenant filter

## Events

**Event**:
One bazaar occurrence with a date and an optional order cutoff. It belongs to exactly one store, and orders never mix between events or between stores.
_Avoid_: Bazaar (as a data term), session

**Active Event**:
The event that new orders default to when the message does not name one. Exactly one event per store is active at a time.
_Avoid_: Current bazaar, selected event

**Test Event**:
An event created to rehearse the order flow. It has its own orders and never affects a real event's summary.
_Avoid_: Dummy event, sandbox

**Cutoff**:
The date and time after which orders would normally close. It is informational only; the assistant never rejects an order because of it.
_Avoid_: Deadline, closing time

## Menu

**Menu**:
The set of products and prices offered for one event. Each event has its own menu and prices; a product catalog that belongs to the store supplies names, categories, and aliases.
_Avoid_: Global menu, one price list for all events

**Category**:
A menu grouping — Makanan, Cemilan, Minuman — used for display.
_Avoid_: Section, group

**Product**:
A sellable catalog item belonging to one store, with one canonical name, a category, and a default integer Rupiah price. The price actually charged comes from the event's menu.
_Avoid_: Item (use Order Item), SKU, dish

**Flavor**:
A choice within a drink line, such as Gurin, Orenji, Original, or Honey Lime.
_Avoid_: Variant, type

**Alias**:
An alternative name, abbreviation, or common typo that points to a product within its store. Aliases are suggestions; an ambiguous alias is confirmed with the user.
_Avoid_: Synonym, nickname

**Sold Out**:
A product is unavailable for one event. Sold-out is per event, not global.
_Avoid_: Unavailable (global), out of stock

## People and roles

**Customer**:
The person an order is for, identified by name only. Not the store member who relays the order, and never an account: a customer does not sign in.
_Avoid_: Client, buyer, user, account

**Relayed Order**:
An order message that a store member forwards or posts on a customer's behalf. It must name the customer.
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
The explicit `OK <4-digit code>` reply that authorizes committing a draft. The code belongs to one draft in one store and dies with it. A plain "yes" is not confirmation.
_Avoid_: Approval, yes, agreement

**Edit**:
A draft that changes an order's items or notes after confirmation. Editing a paid order resets it to unpaid.
_Avoid_: Modification, update

**Cancellation**:
The soft state change that marks an order cancelled while keeping the order and its history.
_Avoid_: Delete, removal

## Money and reporting

**Paid / Unpaid**:
Whether the store has received cash for a confirmed order. Payment is separate from order status; a cancelled order can still have been paid, which needs a manual refund.
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
The append-only record of order actions: created, edited, cancelled, paid, unpaid. It records which account acted and never a channel identity.
_Avoid_: History, changelog
