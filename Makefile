BINARY := conduitd
ADDR   ?= :8080

.PHONY: up down logs rebuild run run-dev ui build vet fmt test test-race test-pg clean

## -------- Docker: one command to build & run everything (app + DB) --------

up:         ## Build and run the whole stack (backend + UI + Postgres). Ctrl-C to stop.
	docker compose up --build

down:       ## Stop the stack (keep data). Use `make down-v` wording below to drop data.
	docker compose down

down-v:     ## Stop the stack AND delete the Postgres volume (fresh state)
	docker compose down -v

logs:       ## Tail logs from the running stack
	docker compose logs -f

rebuild:    ## Rebuild images from scratch and run (no cache)
	docker compose build --no-cache && docker compose up

## -------- Local (requires Go 1.24+ and Node 20.9+) --------

ui:         ## Build the Next.js UI into web/out (embedded by the Go binary)
	cd web && npm install && npm run build

build: ui   ## Build the single static binary (UI embedded) — run `make ui` first on a clean checkout
	CGO_ENABLED=0 go build -o $(BINARY) .

run:        ## Run the server from the embedded UI (http://localhost:8080); set CONDUIT_DATABASE_URL for Postgres
	CONDUIT_ADDR=$(ADDR) go run .

run-dev:    ## Hot-reload UI dev server on :3000 proxying the API; run `make run` in another shell for the API
	cd web && NEXT_PUBLIC_API_BASE=http://localhost:8080 npm run dev

vet:        ## Static checks
	go vet ./...

fmt:        ## Format Go
	gofmt -w .

test:       ## Run Go tests (Postgres tests skip without a DB)
	go test ./...

test-race:  ## Run Go tests under the race detector
	go test -race ./...

test-pg:    ## Run Go tests including the Postgres contract (needs CONDUIT_TEST_POSTGRES)
	go test ./...

clean:
	rm -f $(BINARY)
	rm -rf web/out web/.next
