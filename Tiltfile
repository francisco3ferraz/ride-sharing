# `tilt up` builds every service, deploys it to the local cluster, and rebuilds on save.
# First time: `make cluster certs`. Then the API is at https://ride-sharing.localhost/v1/...

load('ext://helm_resource', 'helm_resource', 'helm_repo')

if k8s_context() != 'kind-ride-sharing':
    fail('kubectl points at %s. Run `make cluster`, which also switches to kind-ride-sharing.' % k8s_context())

GATEWAY_API_VERSION = 'v1.6.1'  # the version Traefik's chart documents
TRAEFIK_CHART_VERSION = '41.7.1'


# Platform: Gateway API, Traefik and the public Gateway.

local_resource(
    'gateway-api-crds',
    cmd='kubectl apply --server-side -f https://github.com/kubernetes-sigs/gateway-api/releases/download/%s/standard-install.yaml' % GATEWAY_API_VERSION,
    labels=['platform'],
)

helm_repo('traefik', 'https://traefik.github.io/charts', resource_name='traefik-repo', labels=['platform'])
helm_resource(
    'traefik',
    'traefik/traefik',
    namespace='traefik',
    flags=['--version', TRAEFIK_CHART_VERSION, '--create-namespace', '--values', 'infra/k8s/platform/traefik/values.yaml'],
    deps=['infra/k8s/platform/traefik/values.yaml'],
    resource_deps=['traefik-repo', 'gateway-api-crds'],
    labels=['platform'],
)

CERT, KEY = 'certs/ride-sharing.pem', 'certs/ride-sharing-key.pem'
if not (os.path.exists(CERT) and os.path.exists(KEY)):
    fail('No HTTPS certificate yet. Run `make certs` once.')
watch_file(CERT)
# Built from the local files on every `tilt up`, so the key is never committed.
k8s_yaml(local(
    'kubectl create secret tls ride-sharing-tls --namespace traefik --cert %s --key %s --dry-run=client -o yaml' % (CERT, KEY),
    quiet=True,
    echo_off=True,
))
k8s_yaml(kustomize('infra/k8s/platform/gateway'))
k8s_resource(
    new_name='gateway',
    objects=['ride-sharing-tls:secret', 'public:gateway', 'https-redirect:httproute'],
    resource_deps=['traefik'],
    labels=['platform'],
)


# Services.

k8s_yaml(kustomize('infra/k8s/overlays/local'))


def go_service(name, objects=[], resource_deps=[], links=[]):
    docker_build(
        name,
        context='.',
        dockerfile='services/%s/Dockerfile' % name,
        # Rebuild only when files this service compiles from change.
        only=[p for p in ['go.mod', 'go.sum', 'gen', 'shared', 'services/' + name] if os.path.exists(p)],
    )
    k8s_resource(name, objects=objects, resource_deps=resource_deps, links=links, labels=['services'])


go_service('pricing')
go_service(
    'edge',
    objects=['edge:httproute'],
    resource_deps=['gateway-api-crds'],  # the HTTPRoute needs the Gateway API types
    links=['https://ride-sharing.localhost/v1/fare-estimate?distance_meters=5000&duration_seconds=600'],
)
