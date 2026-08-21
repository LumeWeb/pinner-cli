// Package account generates a contract-accurate fake server for the Pinner
// account/portal API (auth, account, API keys), derived from the vendored
// swagger spec in ../specs/account.yaml. It is used as the upstream API test
// double for pinner-cli's end-to-end MCP tests.
//
//go:generate go tool oapi-codegen -config oai-codegen.yaml ../specs/account.yaml
//go:generate python3 ../../../scripts/gen_server_stub.py server.gen.go account serverStub.gen.go
package account
