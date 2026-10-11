.PHONY: generate lint lint-openapi test cluster cluster-down certs up

# Redocly CLI lints the OpenAPI spec, as buf lint does the protos. CI runs it through
# `make lint-openapi`, so this pin is the only one.
REDOCLY := npx --yes @redocly/cli@2.54.3

# sqlc isn't a Go tool dependency (it needs cgo and has a large dependency graph), so
# it's installed separately. Its version is written into every file it generates, so a
# different local version would fail CI's generated-code check; generate checks first.
# sqlc also never deletes the file of a removed query, so its output folders are wiped
# before it runs, as buf wipes gen/.
SQLC_VERSION := 1.31.1

## generate: regenerate gen/ from proto/, the Edge's API code from openapi/, and each
## service's internal/db from its queries/
generate:
	@sqlc version 2>/dev/null | grep -qx 'v$(SQLC_VERSION)' || \
		{ echo "make generate needs sqlc v$(SQLC_VERSION), found: $$(sqlc version 2>&1)"; exit 1; }
	buf generate
	go tool oapi-codegen -config openapi/oapi-codegen.yaml openapi/openapi.yaml
	rm -rf services/*/internal/db
	sqlc generate

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
