.PHONY: generate lint test

## generate: regenerate gen/ from proto/
generate:
	buf generate

## lint: run every check CI runs, except tests
lint:
	buf lint
	buf format --diff --exit-code
	go mod tidy -diff
	golangci-lint run ./...

## test: run Go tests with the race detector
test:
	go test -race ./...
