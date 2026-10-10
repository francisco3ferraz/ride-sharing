# ride-sharing

A ride-sharing web app for Lisbon. Riders book rides, drivers accept and complete them, and payments are mocked. The backend is seven Go microservices behind an Edge (API gateway), with a React and TypeScript frontend, running locally on Kubernetes.

## Status

**Step 1 of 6 done: foundations.** What works today:

- A kind cluster with a local image registry, driven by Tilt: one command builds, deploys and rebuilds on save.
- HTTPS through Traefik and the Kubernetes Gateway API, with a locally trusted certificate.
- Two services: the **Edge**, which exposes REST, and **Pricing**, which it calls over gRPC to estimate fares.
- Tracing: every request is one trace from Traefik through the Edge to Pricing, viewable in Jaeger and Grafana. Prometheus and Loki are running too; Traefik's metrics already flow, and the services' own metrics and logs come next.
- CI on every pull request: build, tests, linting, and protobuf lint and breaking-change checks.

Next: identity and authentication.

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

The whole local stack uses about 2.5 GB of memory. Any component can be switched off in Tilt's UI.

| What | Where |
|---|---|
| API | `https://ride-sharing.localhost/v1` |
| Grafana (traces, metrics and logs) | `https://grafana.ride-sharing.localhost` |
| Jaeger | `https://jaeger.ride-sharing.localhost` |
| Prometheus | `https://prometheus.ride-sharing.localhost` |

`make cluster-down` deletes the cluster.

## How it works

```
browser ─https─▶ Traefik (Gateway API) ─▶ Edge (REST) ─gRPC─▶ Pricing
```

The browser only ever talks to the Edge, through Traefik. The Edge holds no business logic: it translates REST into gRPC calls to the services that own each decision.

Traefik and every service send their telemetry over OTLP to the OpenTelemetry Collector, which forwards traces to Jaeger, metrics to Prometheus and logs to Loki. Grafana shows all three in one place.

## Development

| Command | What it does |
|---|---|
| `make generate` | Regenerate Go code in `gen/` from the `.proto` files |
| `make lint` | Run Buf lint and format checks, the `go.mod` tidy check and golangci-lint |
| `make test` | Run Go tests with the race detector |

## Repository layout

```
services/<name>/   one folder per service: main.go, internal/, Dockerfile
shared/            plumbing shared by services (telemetry)
proto/             gRPC contracts, managed with Buf
gen/               generated Go code, never edited by hand
infra/             cluster config, Kubernetes manifests, Helm values
```

## License

[MIT](LICENSE)
