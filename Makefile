COMPOSE    := docker compose
BROKERS    ?= localhost:19092
DB_DSN     ?= postgres://postgres:postgres@localhost:55432/risk?sslmode=disable
ADMIN_DSN  ?= postgres://postgres:postgres@localhost:55432/postgres?sslmode=disable
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

.DEFAULT_GOAL := help

## help: list the targets
.PHONY: help
help:
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':'

## build: build bin/risk with the build time stamped in
.PHONY: build
build:
	go build -ldflags "-X main.buildTime=$(BUILD_TIME)" -o bin/risk ./cmd/risk

## image: build the container image, tagged risk:<commit>
.PHONY: image
image:
	docker build --build-arg BUILD_TIME=$(BUILD_TIME) -t risk:$(shell git rev-parse --short HEAD) .

## test: run the unit tests with the race detector
.PHONY: test
test:
	go test -race -count=1 ./...

## audit: check formatting, vet, and that go.mod is tidy and verified
.PHONY: audit
audit:
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }
	go vet ./...
	go vet -tags e2e ./e2e
	go mod tidy -diff
	go mod verify

## stack/up: start Kafka and Postgres for local runs and tests
.PHONY: stack/up
stack/up:
	$(COMPOSE) up -d --wait

## stack/down: stop and remove the local stack and its data
.PHONY: stack/down
stack/down:
	$(COMPOSE) down -v

.PHONY: stack/topics
stack/topics: stack/up
	@for t in txs.incoming txs.incoming.dlq txs.evaluated; do \
		$(COMPOSE) exec -T kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server $(BROKERS) \
			--create --if-not-exists --topic $$t --partitions 6 --replication-factor 1 >/dev/null; \
	done

.PHONY: db/create
db/create: stack/up
	@$(COMPOSE) exec -T postgres psql -U postgres -qtAc "select 1 from pg_database where datname = 'risk'" | grep -q 1 || \
		$(COMPOSE) exec -T postgres psql -U postgres -qc "create database risk"

## migrate/up: apply pending migrations to the local database
.PHONY: migrate/up
migrate/up: build db/create
	RISK_DB_DSN='$(DB_DSN)' bin/risk migrate up

## migrate/version: show the local database's migration version
.PHONY: migrate/version
migrate/version: build stack/up
	RISK_DB_DSN='$(DB_DSN)' bin/risk migrate version

## run: run one instance locally against the stack, TLS and auth off
.PHONY: run
run: migrate/up stack/topics
	RISK_DB_DSN='$(DB_DSN)' bin/risk run -config config.json \
		-kafka-brokers $(BROKERS) -kafka-tls off -auth off -http-tls off

## e2e: run the database and end-to-end tests against the stack (left running for reruns)
.PHONY: e2e
e2e: stack/up
	RISK_TEST_DB_DSN='$(ADMIN_DSN)' go test -count=1 ./db
	RISK_E2E_BROKERS=$(BROKERS) RISK_E2E_DB_DSN='$(ADMIN_DSN)' go test -tags e2e -count=1 -timeout 10m ./e2e

## demo: send scenarios that trigger rules to a running instance (make run) and show the results
.PHONY: demo
demo:
	demo/run.sh

## loadtest: run the load test; pass options with ARGS="-n 20000 -instances 3"
.PHONY: loadtest
loadtest: stack/up
	loadtest/run.sh $(ARGS)

## golden: regenerate golden files after an intended format change; review the diff
.PHONY: golden
golden:
	go test ./risk/contract ./db -update
