// Package ipfs generates a contract-accurate fake server for the Pinner
// content API (IPFS pinning, files, websites, DNS, IPNS, upload), derived from
// the vendored swagger spec in ../specs/ipfs.yaml. It is used as the upstream
// API test double for pinner-cli's end-to-end MCP tests.
//
//go:generate go tool oapi-codegen -config oai-codegen.yaml ../specs/ipfs.yaml
//go:generate python3 ../../../scripts/gen_server_stub.py server.gen.go ipfs serverStub.gen.go
package ipfs
