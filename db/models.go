package db

// Event is one bazaar occurrence.
type Event struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	EventDate string `json:"event_date"`
	CutoffAt  *int64 `json:"cutoff_at,omitempty"`
	IsActive  bool   `json:"is_active"`
	CreatedAt int64  `json:"created_at"`
}

// EventView adds summary counters for list_events.
type EventView struct {
	Event
	OrderCount int   `json:"order_count"`
	Omzet      int64 `json:"omzet"`
}

// Product is a sellable menu item.
type Product struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Price       int64  `json:"price"`
	IsAvailable bool   `json:"is_available"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// MenuProduct is a product with per-event availability and aliases.
type MenuProduct struct {
	Product
	Available bool     `json:"available"`
	Aliases   []string `json:"aliases,omitempty"`
}

// ProductMatch is a product candidate for find_product.
type ProductMatch struct {
	Product
	Available bool `json:"available"`
}

// MenuRow is one row from menu.csv.
type MenuRow struct {
	Category string
	Name     string
	Price    int64
	Aliases  []string
}

// Customer is a name-only customer.
type Customer struct {
	ID             int64  `json:"id"`
	DisplayName    string `json:"display_name"`
	NormalizedName string `json:"normalized_name"`
	CreatedAt      int64  `json:"created_at"`
}

// CustomerMatch is a customer candidate with confirmed order count.
type CustomerMatch struct {
	Customer
	OrderCount int `json:"order_count"`
}

// Draft is a pending create, edit, or cancel.
type Draft struct {
	ID           string  `json:"id"`
	ShortCode    string  `json:"short_code"`
	Kind         string  `json:"kind"`
	EventID      string  `json:"event_id"`
	OrderID      *int64  `json:"order_id,omitempty"`
	CustomerID   *int64  `json:"customer_id,omitempty"`
	CustomerName *string `json:"customer_name,omitempty"`
	Payload      string  `json:"payload"`
	Total        *int64  `json:"total,omitempty"`
	Status       string  `json:"status"`
	ExpiresAt    int64   `json:"expires_at"`
	CreatedAt    int64   `json:"created_at"`
	CommittedAt  *int64  `json:"committed_at,omitempty"`
}

// DraftItem is a priced item inside a draft payload.
type DraftItem struct {
	ProductID int64  `json:"product_id"`
	Name      string `json:"name"`
	Quantity  int    `json:"quantity"`
	UnitPrice int64  `json:"unit_price"`
	Note      string `json:"note,omitempty"`
}

// DraftPayload is the JSON stored in drafts.payload.
type DraftPayload struct {
	Items []DraftItem `json:"items,omitempty"`
	Notes string      `json:"notes,omitempty"`
}

// OrderItemDetail is one item in a confirmed order.
type OrderItemDetail struct {
	ProductID   int64  `json:"product_id"`
	ProductName string `json:"product_name"`
	Quantity    int    `json:"quantity"`
	UnitPrice   int64  `json:"unit_price"`
	Note        string `json:"note,omitempty"`
}

// OrderDetail is a full order with customer, event, and items.
type OrderDetail struct {
	ID           int64             `json:"id"`
	Code         string            `json:"code"`
	EventID      string            `json:"event_id"`
	EventName    string            `json:"event_name"`
	EventDate    string            `json:"event_date"`
	CustomerID   int64             `json:"customer_id"`
	CustomerName string            `json:"customer_name"`
	Status       string            `json:"status"`
	IsPaid       bool              `json:"is_paid"`
	PaidAt       *int64            `json:"paid_at,omitempty"`
	Notes        string            `json:"notes,omitempty"`
	CreatedAt    int64             `json:"created_at"`
	UpdatedAt    int64             `json:"updated_at"`
	CancelledAt  *int64            `json:"cancelled_at,omitempty"`
	Items        []OrderItemDetail `json:"items"`
	Total        int64             `json:"total"`
}

// OrderRow is a compact search result.
type OrderRow struct {
	ID           int64  `json:"id"`
	Code         string `json:"code"`
	CustomerName string `json:"customer_name"`
	Status       string `json:"status"`
	IsPaid       bool   `json:"is_paid"`
	Total        int64  `json:"total"`
}

// SummaryItem is one item line in a summary.
type SummaryItem struct {
	ProductName string `json:"product_name"`
	Quantity    int    `json:"quantity"`
	UnitPrice   int64  `json:"unit_price"`
	Note        string `json:"note,omitempty"`
}

// SummaryOrder is one customer's order in a summary.
type SummaryOrder struct {
	OrderID      int64         `json:"order_id"`
	Code         string        `json:"code"`
	CustomerName string        `json:"customer_name"`
	IsPaid       bool          `json:"is_paid"`
	Total        int64         `json:"total"`
	Items        []SummaryItem `json:"items"`
}

// ProductTotal is the per-product aggregate.
type ProductTotal struct {
	Name     string `json:"name"`
	Quantity int    `json:"quantity"`
	Total    int64  `json:"total"`
}

// Summary is the full report for one event.
type Summary struct {
	Event              Event          `json:"event"`
	Orders             []SummaryOrder `json:"orders"`
	ProductTotals      []ProductTotal `json:"product_totals"`
	OrderCount         int            `json:"order_count"`
	Omzet              int64          `json:"omzet"`
	PaidTotal          int64          `json:"paid_total"`
	UnpaidTotal        int64          `json:"unpaid_total"`
	CancelledCount     int            `json:"cancelled_count"`
	CancelledPaidTotal int64          `json:"cancelled_paid_total"`
}
