// File: test/e2e/doc.go
//
// Package e2e compiles the server, runs it on a free port against a real
// database, and drives it over HTTP — the layer that proves routing,
// middleware ordering and the auth gates behave as deployed.
//
// Every test file in this package carries the `e2e` build tag. Run them with:
//
//	make test-db-up
//	make test-e2e
//
// This file itself is deliberately untagged, so that `go test ./test/e2e/...`
// resolves even when no tagged files are present.
//
// See docs/TESTING.md.
package e2e
