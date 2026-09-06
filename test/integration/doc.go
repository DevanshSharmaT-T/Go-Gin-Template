// File: test/integration/doc.go
//
// Package integration holds tests that boot the real dependency-injection
// graph against a real PostgreSQL database.
//
// Every test file in this package carries the `integration` build tag, so the
// default `go test ./...` stays fast and needs no database. Run them with:
//
//	make test-db-up
//	make test-integration
//
// This file itself is deliberately untagged: it declares the package so that
// `go test ./test/integration/...` resolves even when no tagged files are
// present, which keeps CI honest rather than red.
//
// See docs/TESTING.md.
package integration
