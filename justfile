set shell := ["bash", "-cu"]

default: test

build:
    go build ./...

test:
    go test -race ./...

test-pg dsn:
    KANBOARD_TEST_PG_DSN="{{dsn}}" go test -race ./...

lint:
    golangci-lint run ./...

gen:
    sqlc generate
    go generate ./...
    go mod tidy

fmt:
    gofmt -l -w .
