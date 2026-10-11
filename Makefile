.PHONY: generate lint lint-openapi test cluster cluster-down certs up

# Redocly CLI lints the OpenAPI spec, as buf lint does the protos. CI runs it through
# `make lint-openapi`, so this pin is the only one.
REDOCLY := npx --yes @redocly/cli@2.54.3

## generate: regenerate gen/ from proto/, and the Edge's API code from openapi/
generate:
	buf generate
	go tool oapi-codegen -config openapi/oapi-codegen.yaml openapi/openapi.yaml

## lint: run every check CI runs, except tests
lint: lint-openapi
	buf lint
	buf format --diff --exit-code
	go mod tidy -diff
	golangci-lint run ./...

## lint-openapi: lint the OpenAPI spec with the rules in openapi/redocly.yaml
lint-openapi:
	$(REDOCLY) lint --config openapi/redocly.yaml openapi/openapi.yaml

## test: run Go tests with the race detector
test:
	go test -race ./...

## cluster: create the local kind cluster and its image registry
cluster:
	ctlptl apply -f infra/cluster/ctlptl.yaml

## cluster-down: delete the local cluster and its registry
cluster-down:
	ctlptl delete -f infra/cluster/ctlptl.yaml

## certs: create the locally trusted HTTPS certificate (once per machine)
certs:
	mkdir -p certs
	mkcert -cert-file certs/ride-sharing.pem -key-file certs/ride-sharing-key.pem \
		ride-sharing.localhost "*.ride-sharing.localhost"

## up: build and run everything in the cluster, rebuilding on save
up:
	tilt up
