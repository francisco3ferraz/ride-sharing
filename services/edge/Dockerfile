# syntax=docker/dockerfile:1

# Build from the repo root: docker build -f services/edge/Dockerfile .
FROM golang:1.27.2 AS build
WORKDIR /src

# Dependencies get their own layer, so code changes don't re-download them.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -o /out/edge ./services/edge

# Distroless: just the binary, no shell or package manager, running as a non-root user.
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/edge /edge
ENTRYPOINT ["/edge"]
