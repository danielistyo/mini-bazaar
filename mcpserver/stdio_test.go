package mcpserver_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"bazaar-minggu/db"
)

func seedFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdio.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	ctx := context.Background()
	if err := d.CreateEvent(ctx, db.Event{
		ID:        "BAZAR-2026-11-15",
		Name:      "Bazar KPKM",
		EventDate: "2026-11-15",
		CreatedAt: time.Now().Unix(),
	}); err != nil {
		t.Fatalf("create event: %v", err)
	}
	if err := d.SetActiveEvent(ctx, "BAZAR-2026-11-15"); err != nil {
		t.Fatalf("activate event: %v", err)
	}
	if _, err := d.SeedProducts(ctx, "BAZAR-2026-11-15", []db.MenuRow{
		{Category: "Makanan", Name: "Sate babi 5 tusuk", Price: 43000, Aliases: []string{"sate"}},
	}); err != nil {
		t.Fatalf("seed menu: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	return path
}

func TestStdioServer(t *testing.T) {
	path := seedFile(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "run", "../cmd/bazaar-mcp", "serve")
	cmd.Env = append(os.Environ(), "BAZAAR_DB="+path)
	client := mcp.NewClient(&mcp.Implementation{Name: "stdio-test", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) != 17 {
		t.Fatalf("tools = %d, want 17", len(tools.Tools))
	}

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_events",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("call list_events: %v", err)
	}
	if res.IsError {
		t.Fatalf("list_events is error: %+v", res.Content)
	}
}
