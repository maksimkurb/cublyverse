// Package api exposes the OpenAPI specification as compiled-in data.
//
// The specification file `openapi.yaml` is the single source of truth for the
// HTTP API. Keeping the embed directive in the same directory means the file
// served at `/api/openapi.yaml` is always the exact document the Go server and
// the TypeScript client were generated from.
package api

import _ "embed"

// OpenAPISpec is the raw content of `api/openapi.yaml`.
//
//go:embed openapi.yaml
var OpenAPISpec []byte
