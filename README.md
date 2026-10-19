# ride-sharing

A ride-sharing web app for Lisbon. Riders book rides, drivers accept and complete them, and payments are mocked. The backend is seven Go microservices behind an Edge (API gateway), with a React and TypeScript frontend, running locally on Kubernetes.

## Quick start

You need Docker, kubectl, [kind](https://kind.sigs.k8s.io/), [ctlptl](https://github.com/tilt-dev/ctlptl), [Tilt](https://tilt.dev/), [Helm](https://helm.sh/) and [mkcert](https://github.com/FiloSottile/mkcert). Run `mkcert -install` once so your machine trusts local certificates.

```bash
make cluster   # create the kind cluster and its image registry
make certs     # create the HTTPS certificate (once per machine)
make up        # build and deploy everything; Tilt's UI is at http://localhost:10350
```

Then ask for a fare estimate:

```bash
curl 'https://ride-sharing.localhost/v1/fare-estimate?distance_meters=5000&duration_seconds=600'
# {"fare_cents":750}
```

Errors come back as [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem details:

```bash
curl 'https://ride-sharing.localhost/v1/fare-estimate?distance_meters=-5&duration_seconds=600'
# {"detail":"distance_meters must be positive","status":400,"title":"Bad Request","type":"about:blank"}
```

The whole local stack uses about 3 GB of memory. Any component can be switched off in Tilt's UI.

| What | Where |
|---|---|
| API | `https://ride-sharing.localhost/v1` |
| Grafana (traces, metrics and logs) | `https://grafana.ride-sharing.localhost` |
| Jaeger | `https://jaeger.ride-sharing.localhost` |
| Prometheus | `https://prometheus.ride-sharing.localhost` |

`make cluster-down` deletes the cluster.

## How it works

```
browser ─https─▶ Traefik (Gateway API) ─▶ Edge (REST) ─gRPC─▶ services ─▶ Postgres
                                           │
                                           └─▶ Valkey (rate limits)
```

The browser only ever talks to the Edge, through Traefik. The Edge holds no business logic: it translates REST into gRPC calls to the services that own each decision.

**Requests are rate limited at the Edge, and logins locked out in Identity.** The Edge limits requests per client address, per logged-in user, and per route for login, registration and password reset. It keeps its counts in [Valkey](https://valkey.io/), so every copy of the Edge shares them. A request over a limit gets a 429 with `Retry-After`. If Valkey can't answer, requests go through and the failures are counted in a metric. Account protection doesn't depend on Valkey: Identity counts failed logins per email and client address in its own database, and five within 15 minutes lock that pair out for 15 minutes, whether or not the email is registered.

**Identity turns password hashes away rather than queue them for too long.** Registration, login and password reset each hash a password with argon2id, which takes 19 MiB and tens of milliseconds, so Identity hashes four at a time and lets 16 more wait. It turns any more away at once, and the Edge answers 503: a longer queue would spend every hash on a request about to time out. A service the Edge can't reach gets the same 503, with the same fixed message.

**The REST API is spec-first.** [`openapi/openapi.yaml`](openapi/openapi.yaml) is the contract. The Edge's handler interfaces are generated from it with [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen), every request is validated against it before a handler runs, and Redocly lints it in CI.

**Each service owns its data.** Postgres runs in the cluster under the [CloudNativePG](https://cloudnative-pg.io/) operator, with one database and one role per service, so no service can read another's tables. Valkey likewise gives each service its own user, limited to its own keys, and keeps nothing that can't be lost. A service's schema migrations are compiled into its binary and applied by an init container before the pod starts.

**Every request is one trace.** Traefik and every service send their telemetry over OTLP to the OpenTelemetry Collector, which forwards traces to Jaeger, metrics to Prometheus and logs to Loki. Grafana shows all three in one place.

## Development

| Command | What it does |
|---|---|
| `make generate` | Regenerate Go code from the `.proto` files, the OpenAPI spec and each service's SQL queries (needs Buf and [sqlc](https://sqlc.dev/) 1.31.1) |
| `make lint` | Lint the spec (Redocly, through `npx`), run Buf lint and format checks, the `go.mod` tidy check and golangci-lint |
| `make test` | Run Go tests with the race detector |

Generated code is committed, and CI fails if it's out of date.

## Repository layout

```
services/<name>/   one folder per service: main.go, internal/, migrations/, queries/, Dockerfile
shared/            plumbing shared by services (telemetry, the gRPC server)
proto/             gRPC contracts, managed with Buf
openapi/           the REST contract and its code generation and lint config
gen/               Go code generated from proto/, never edited by hand
infra/             cluster config, Kubernetes manifests, Helm values
```

## License

[MIT](LICENSE)
