# Go + Gin Backend Template
#
# Targets are grouped: development, build, test, quality, docs.
# `make help` lists everything with its description.

BINARY      := server
CMD         := ./cmd/api
BIN_DIR     := bin

# Disposable database for the DB-backed suites. Overridable:
#   make test-integration TEST_DATABASE_URL=postgres://...
TEST_DATABASE_URL ?= postgres://postgres:test@127.0.0.1:55433/app_test?sslmode=disable
TEST_DB_CONTAINER ?= gin_template_test_db
export TEST_DATABASE_URL

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# --- Development -------------------------------------------------------------

.PHONY: dev
dev: ## Run with live reload (air); GO_ENV=development
	air

.PHONY: run
run: ## Run once, without live reload
	GO_ENV=development go run $(CMD)

# --- Build -------------------------------------------------------------------

.PHONY: build
build: ## Build a static linux/amd64 binary into bin/
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
		go build -trimpath -ldflags="-w -s" -o $(BIN_DIR)/$(BINARY) $(CMD)

.PHONY: clean
clean: ## Remove build output and temporary files
	rm -rf tmp $(BIN_DIR) coverage.out

.PHONY: tidy
tidy: ## Sync go.mod/go.sum with the imports actually used
	go mod tidy

# --- Test --------------------------------------------------------------------
# Layered suites; see docs/TESTING.md. The DB-backed layers are behind build
# tags so the default `go test ./...` needs no database.

.PHONY: test
test: ## Unit + package tests, race detector, no database
	go test ./... -race

.PHONY: test-cover
test-cover: ## Same as `test`, with a coverage profile in coverage.out
	go test ./... -race -coverprofile=coverage.out -covermode=atomic
	@go tool cover -func=coverage.out | tail -1

.PHONY: test-integration
test-integration: ## Real fx graph against a real PostgreSQL  [tag: integration]
	go test -tags=integration ./test/integration/... -count=1 -v

.PHONY: test-e2e
test-e2e: ## The compiled server, driven over HTTP  [tag: e2e]
	go test -tags=e2e ./test/e2e/... -count=1 -v -timeout 10m

.PHONY: test-all
test-all: test test-integration test-e2e ## Every suite

.PHONY: test-db-up
test-db-up: ## Start a disposable PostgreSQL on :55433
	docker run -d --name $(TEST_DB_CONTAINER) \
		-e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=test -e POSTGRES_DB=app_test \
		-p 55433:5432 postgres:16
	@echo "waiting for postgres..."
	@# -h 127.0.0.1 is load-bearing. The postgres image runs a temporary server
	@# during initialisation with listen_addresses='' — reachable over the Unix
	@# socket but not over TCP — then shuts it down and starts the real one. A
	@# plain `pg_isready` therefore reports ready roughly a second before the
	@# server the tests connect to exists, and the suite starts against a
	@# database that is about to restart under it. Asking over TCP is what tells
	@# the two phases apart.
	@n=0; until docker exec $(TEST_DB_CONTAINER) \
			pg_isready -h 127.0.0.1 -U postgres -d app_test >/dev/null 2>&1; do \
		n=$$((n+1)); \
		if [ $$n -ge 60 ]; then \
			echo "postgres did not become ready in 60s; try: docker logs $(TEST_DB_CONTAINER)" >&2; \
			exit 1; \
		fi; \
		sleep 1; \
	done
	@echo "ready: $(TEST_DATABASE_URL)"

.PHONY: test-db-down
test-db-down: ## Remove the disposable PostgreSQL container
	-docker rm -f $(TEST_DB_CONTAINER)

.PHONY: mocks
mocks: ## Regenerate repository mocks into internal/mocks
	go run go.uber.org/mock/mockgen@v0.6.0 \
		-source=internal/modules/users/domain/user_repository.go \
		-destination=internal/mocks/users/mock_user_repository.go -package=users
	go run go.uber.org/mock/mockgen@v0.6.0 \
		-source=internal/modules/users/domain/verification_token_repository.go \
		-destination=internal/mocks/users/mock_verification_token_repository.go -package=users
	go run go.uber.org/mock/mockgen@v0.6.0 \
		-source=internal/modules/roles/domain/role_repository.go \
		-destination=internal/mocks/roles/mock_role_repository.go -package=roles

# --- Quality -----------------------------------------------------------------

.PHONY: vet
vet: ## go vet — the shared baseline, always enforced in CI
	go vet ./...

.PHONY: lint
lint: ## golangci-lint using the committed .golangci.yml
	golangci-lint run

.PHONY: vuln
vuln: ## Scan dependencies and the toolchain for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

.PHONY: check
check: vet test ## What CI runs on every push

# --- Docs --------------------------------------------------------------------

.PHONY: swagger
swagger: ## Regenerate the OpenAPI spec into docs/
	go run github.com/swaggo/swag/cmd/swag@latest init -g cmd/api/main.go -o docs

.PHONY: tools
tools: ## Install the optional local tools (air, golangci-lint, swag)
	go install github.com/air-verse/air@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/swaggo/swag/cmd/swag@latest
