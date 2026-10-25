"""
ConfigMap for sthings-portal
"""

import labels

config = labels.config

configMap = {
    apiVersion: "v1"
    kind: "ConfigMap"
    metadata: {
        name: "{}-config".format(config.name)
        namespace: config.namespace
        labels: labels.commonLabels
    }
    data: {
        LOG_LEVEL: "info"
        PORTAL_SERVICES: ",".join(config.services)
        HEALTH_INTERVAL: config.healthInterval
        HEALTH_TIMEOUT: config.healthTimeout
        **config.extraEnvVars
    }
}
