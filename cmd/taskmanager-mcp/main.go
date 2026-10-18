package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mhdph/taskmanager/internal/mcpbridge"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	// stdout belongs exclusively to the MCP protocol. log writes to stderr.
	client, err := mcpbridge.NewClient(mcpbridge.Config{
		APIURL: os.Getenv("TASKMANAGER_API_URL"), APIKey: os.Getenv("TASKMANAGER_API_KEY"),
		UserID: os.Getenv("TASKMANAGER_USER_ID"), AppID: os.Getenv("TASKMANAGER_APP_ID"),
	})
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := mcpbridge.NewServer(client).Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
