"""
HTTPRoute (Gateway API) for sthings-portal

The portal takes the whole hostname: its own pages at /, and every service
under /<key>/ through its reverse proxy. The services need no route of their
own for that.
"""

import labels

config = labels.config

_hostname = config.httpRouteHostname

httproute = {
    apiVersion: "gateway.networking.k8s.io/v1"
    kind: "HTTPRoute"
    metadata: {
        name: config.name
        namespace: config.namespace
        labels: labels.commonLabels
        if config.annotations or config.httpRouteAnnotations:
            annotations: {
                **config.annotations
                **config.httpRouteAnnotations
            }
    }
    spec: {
        # group + kind are admission-webhook defaults on parentRefs; rendering
        # them explicitly avoids drift on every reconcile.
        parentRefs: [
            {
                group: "gateway.networking.k8s.io"
                kind: "Gateway"
                name: config.httpRouteParentRefName
                if config.httpRouteParentRefNamespace:
                    namespace: config.httpRouteParentRefNamespace
            }
        ]
        if _hostname:
            hostnames: [_hostname]
        rules: [
            {
                matches: [
                    {
                        path: {
                            type: "PathPrefix"
                            value: "/"
                        }
                    }
                ]
                backendRefs: [
                    {
                        group: ""
                        kind: "Service"
                        name: config.name
                        port: config.servicePort
                        weight: 1
                    }
                ]
            }
        ]
    }
} if config.httpRouteEnabled else None
