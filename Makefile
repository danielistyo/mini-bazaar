BIN := bin/bazaar-mcp
DB := data/orders.db

build:
	CGO_ENABLED=1 go build -o $(BIN) ./cmd/bazaar-mcp

lint:
	go vet ./...

test:
	go test ./...

init: build
	BAZAAR_DB=$(DB) $(BIN) init

seed-menu: build
	@test -n "$(MENU)" || (echo "usage: make seed-menu MENU=menu.csv"; exit 1)
	BAZAAR_DB=$(DB) $(BIN) seed-menu $(MENU)

run: build
	BAZAAR_DB=$(DB) $(BIN) serve

.PHONY: build lint test e2e init seed-menu run

# Required after every deploy. Runs the real binary against a copy of the
# database; a failure means do not restart the bot.
e2e: build
	BAZAAR_DB=$(DB) BAZAAR_BIN=$(BIN) python3 scripts/e2e_backend.py
