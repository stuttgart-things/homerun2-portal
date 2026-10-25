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
        if config.authMode == "oidc":
            AUTH_OIDC_ISSUER: config.authOidcIssuer
            AUTH_OIDC_CLIENT_ID: config.authOidcClientId
            AUTH_OIDC_REDIRECT_URL: config.authOidcRedirectUrl or "https://{}/auth/callback".format(config.httpRouteHostname)
            AUTH_OIDC_NAME: config.authOidcName
        if config.authMode == "oidc" and config.authOidcGroupsClaim:
            AUTH_OIDC_GROUPS_CLAIM: config.authOidcGroupsClaim
        if config.authMode == "oidc" and config.authOidcCaConfigMap:
            AUTH_OIDC_CA_FILE: "/etc/portal/oidc-ca/{}".format(config.authOidcCaKey)
        if config.authMode == "oidc" and config.authOidcAllowedGroups:
            AUTH_OIDC_ALLOWED_GROUPS: ",".join(config.authOidcAllowedGroups)
        if config.title:
            PORTAL_TITLE: config.title
        if config.subtitle:
            PORTAL_SUBTITLE: config.subtitle
        if config.headerImageConfigMap:
            PORTAL_HEADER_IMAGE: "/etc/portal/branding/{}".format(config.headerImageKey)
        **config.extraEnvVars
    }
}
