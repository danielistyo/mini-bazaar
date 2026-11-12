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

.PHONY: build lint test init seed-menu run
