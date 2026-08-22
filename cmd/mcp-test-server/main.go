// Command mcp-test-server boots the swagger-generated fake Pinner API
// (internal/mcptest) on a localhost port. It is the upstream API double used
// by the Sunpeak MCP end-to-end tests: the pinner MCP server is pointed at
// this endpoint so `invoke_tool` calls return real data instead of an
// "authentication required" error.
//
// It is a test-only binary, not part of the production pinner command.
//
// It seeds a deterministic account/token on boot (e2e@example.com /
// token-e2e@example.com) so a pinner config can be pre-provisioned with a
// valid auth token. Override the account with --seed-email.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"go.lumeweb.com/pinner-cli/internal/mcptest"
)

func main() {
	port := flag.String("port", "8125", "listener port")
	seedEmail := flag.String("seed-email", "e2e@example.com", "seeded account email")
	firstName := flag.String("seed-first", "E2E", "seeded account first name")
	lastName := flag.String("seed-last", "Test", "seeded account last name")
	flag.Parse()

	api := mcptest.New()
	api.Seed(*seedEmail, *firstName, *lastName)
	fmt.Printf("mcptest: fake Pinner API on http://127.0.0.1:%s\n", *port)
	// Do not print the token itself (it is a secret-shaped value, even in a
	// deterministic test double); the harness knows it from the fixture config.
	fmt.Printf("mcptest: seeded account %s (authenticated)\n", *seedEmail)

	srv := &http.Server{
		Addr:    "127.0.0.1:" + *port,
		Handler: api.Handler(),
	}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
