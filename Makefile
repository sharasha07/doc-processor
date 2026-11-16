export CGO_ENABLED := 1
export CC := gcc
export CXX := g++
export CGO_LDFLAGS := -L$(CURDIR)/native/$(shell go env GOOS)_$(shell go env GOARCH) -lextractous_ffi
export LD_LIBRARY_PATH := $(CURDIR)/native/$(shell go env GOOS)_$(shell go env GOARCH):$(LD_LIBRARY_PATH)

## help: print this help message
.PHONY: help
help:
	@echo 'Usage:'
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'

## run/api: run the cmd/api application
.PHONY: run/api
run/api:
	dotenvx run -- go run ./cmd/api

## audit: tidy dependencies and format, vet and test all code
.PHONY: audit
audit:
	go mod tidy
	go mod verify
	go fmt ./...
	go vet ./...
	staticcheck ./...
	go test -race -vet=off ./...

## db/psql: connect to the database using psql
.PHONY: db/psql
db/psql:
	dotenvx run -- sh -c 'psql $${DB_URL}'

## db/migrate/create name=$1: create a new database migration
.PHONY: db/migrate/create
db/migrate/create:
	migrate create -ext=.sql -dir=./migrations -seq ${name}

## db/migrate/up: apply all up database migrations
.PHONY: db/migrate/up
db/migrate/up:
	dotenvx run -- sh -c 'migrate -path=./migrations -database=$${DB_URL} up'

## db/migrate/down: resolve all up database migrations
.PHONY: db/migrate/down
db/migrate/down:
	dotenvx run -- sh -c 'migrate -path=./migrations -database=$${DB_URL} down'
