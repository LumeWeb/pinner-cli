// Command mcp-test-server boots the swagger-generated fake Pinner API
// (internal/mcptest) on a localhost port. It is the upstream API double used
// by the Sunpeak MCP end-to-end tests: the pinner MCP server is pointed at
// this endpoint so `invoke_tool` calls return real data instead of an
// "authentication required" error.
//
// It is a test-only binary, not part of the production pinner command.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"go.lumeweb.com/pinner-cli/internal/mcptest"
)

func main() {
	port := "8125"
	if len(os.Args) > 1 {
		port = os.Args[1]
	}
	api := mcptest.New()
	srv := &http.Server{
		Addr:    "127.0.0.1:" + port,
		Handler: api.Handler(),
	}
	fmt.Printf("mcptest: fake Pinner API on http://127.0.0.1:%s\n", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
