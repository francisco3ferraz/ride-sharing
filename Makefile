.PHONY: generate lint test cluster cluster-down certs up

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
