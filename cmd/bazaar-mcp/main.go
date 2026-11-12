package main

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"bazaar-minggu/db"
	"bazaar-minggu/mcpserver"
	"bazaar-minggu/orders"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	dbPath := os.Getenv("BAZAAR_DB")
	if dbPath == "" {
		dbPath = "data/orders.db"
	}

	switch os.Args[1] {
	case "serve":
		serve(serveDBPath(os.Args[2:], dbPath))
	case "init":
		initDB(dbPath)
	case "seed-menu":
		if len(os.Args) < 3 {
			fatal("usage: bazaar-mcp seed-menu <menu.csv>")
		}
		seedMenu(dbPath, os.Args[2])
	default:
		usage()
		os.Exit(2)
	}
}

func serveDBPath(args []string, fallback string) string {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	dbPath := fs.String("db", fallback, "SQLite database path")
	if err := fs.Parse(args); err != nil {
		fatal("parse serve flags: " + err.Error())
	}
	return *dbPath
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: bazaar-mcp <serve|init|seed-menu <menu.csv>>")
}

func serve(dbPath string) {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))

	d, err := db.Open(dbPath)
	if err != nil {
		fatal("open database: " + err.Error())
	}
	defer d.Close()

	server := mcpserver.New(orders.New(d))
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fatal("mcp server: " + err.Error())
	}
}

func initDB(dbPath string) {
	d, err := db.Open(dbPath)
	if err != nil {
		fatal("open database: " + err.Error())
	}
	defer d.Close()

	if err := orders.EnsureInitialEvent(context.Background(), d); err != nil {
		fatal("seed initial event: " + err.Error())
	}
	fmt.Println("initialized", dbPath)
}

func seedMenu(dbPath, menuPath string) {
	d, err := db.Open(dbPath)
	if err != nil {
		fatal("open database: " + err.Error())
	}
	defer d.Close()

	rows, err := readMenu(menuPath)
	if err != nil {
		fatal("read menu: " + err.Error())
	}
	ctx := context.Background()
	event, err := d.GetActiveEvent(ctx)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			fatal("seed menu: belum ada event aktif")
		}
		fatal("seed menu: " + err.Error())
	}
	n, err := d.SeedProducts(ctx, event.ID, rows)
	if err != nil {
		fatal("seed menu: " + err.Error())
	}
	fmt.Printf("seeded %d products into %s\n", n, event.ID)
}

func readMenu(path string) ([]db.MenuRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("menu is empty")
	}

	rows := make([]db.MenuRow, 0, len(records)-1)
	for i, record := range records[1:] {
		if len(record) < 4 {
			return nil, fmt.Errorf("row %d: expected category,name,price,aliases", i+2)
		}
		price, err := strconv.ParseInt(strings.TrimSpace(record[2]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("row %d: price %q is not an integer", i+2, record[2])
		}
		var aliases []string
		for _, a := range strings.Split(record[3], "|") {
			if a = strings.TrimSpace(a); a != "" {
				aliases = append(aliases, a)
			}
		}
		rows = append(rows, db.MenuRow{
			Category: strings.TrimSpace(record[0]),
			Name:     strings.TrimSpace(record[1]),
			Price:    price,
			Aliases:  aliases,
		})
	}
	return rows, nil
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "error:", msg)
	os.Exit(1)
}
