"""
Main entry point for homerun2-portal Kubernetes deployment
Imports all resources and exports manifests
"""

import serviceaccount
import configmap
import deploy
import service
import httproute

# Export all manifests (filter out None values).
# Namespace is NOT emitted from the KCL profile — the consuming Application
# uses `CreateNamespace=true`. See stuttgart-things/homerun2-omni-pitcher#132.
manifests = [
    m for m in [
        serviceaccount.serviceAccount
        configmap.configMap
        deploy.deployment
        service.service
        httproute.httproute
    ] if m
]
