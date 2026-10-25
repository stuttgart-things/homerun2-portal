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
        PORTAL_THEME: config.theme
        AUTH_MODE: config.authMode
        if config.authMode != "none":
            AUTH_USERS_FILE: "/etc/portal/auth/users"
            AUTH_SESSION_TTL: config.authSessionTTL
        if config.title:
            PORTAL_TITLE: config.title
        if config.subtitle:
            PORTAL_SUBTITLE: config.subtitle
        if config.headerImageConfigMap:
            PORTAL_HEADER_IMAGE: "/etc/portal/branding/{}".format(config.headerImageKey)
        **config.extraEnvVars
    }
}
