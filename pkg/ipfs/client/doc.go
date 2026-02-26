// Package ipfsclient provides service wrappers around the generated IPFS client.
//
// This package contains the auto-generated client code from the IPFS HTTP API
// specification (generated via oapi-codegen) and provides service layer wrappers
// that offer a more idiomatic Go interface for interacting with IPFS functionality.
//
// The generated client (client.gen.go) provides low-level HTTP client bindings
// for the IPFS API, while service wrappers in this package will provide:
//   - Simplified interfaces for common operations
//   - Error handling and retry logic
//   - Request/response transformation
//   - Context support for cancellation
//
//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config ../../../oai-codegen.yaml ../../../swagger/ipfs.yaml
//
// Generated Code
//
// The client.gen.go file is auto-generated from the OpenAPI specification
// located at swagger/ipfs.yaml. To regenerate the client:
//
//	go generate ./pkg/ipfs/client
//
// Or run the make target:
//
//	make generate-ipfs
//
// Package Organization
//
// This package is part of the IPFS integration layer for Pinner CLI, which
// provides pinning services and file management capabilities.
package ipfsclient
