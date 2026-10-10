# `tilt up` builds every service, deploys it to the local cluster, and rebuilds on save.
# Create the cluster first with `make cluster`.

if k8s_context() != 'kind-ride-sharing':
    fail('kubectl points at %s. Run `make cluster`, which also switches to kind-ride-sharing.' % k8s_context())

k8s_yaml(kustomize('infra/k8s/overlays/local'))


def go_service(name, port_forwards=[]):
    docker_build(
        name,
        context='.',
        dockerfile='services/%s/Dockerfile' % name,
        # Rebuild only when files this service compiles from change.
        only=[p for p in ['go.mod', 'go.sum', 'gen', 'shared', 'services/' + name] if os.path.exists(p)],
    )
    k8s_resource(name, port_forwards=port_forwards, labels=['services'])


go_service('pricing')
go_service('edge', port_forwards=['8080:8080'])
