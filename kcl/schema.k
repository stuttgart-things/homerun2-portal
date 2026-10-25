"""
Schema definition for sthings-portal deployment configuration
"""

schema Portal:
    """Configuration for sthings-portal Kubernetes deployment"""

    # Metadata
    name: str = "sthings-portal"
    namespace: str = "homerun2"

    # Image
    image: str = "ghcr.io/stuttgart-things/sthings-portal:latest"
    imagePullPolicy: str = "IfNotPresent"

    # Replicas and resources
    replicas: int = 1
    cpuRequest: str = "50m"
    cpuLimit: str = "250m"
    memoryRequest: str = "32Mi"
    memoryLimit: str = "128Mi"

    # Services behind the portal, in navigation order: "key=url" each
    # (PORTAL_SERVICES). Each one is served under /<key>/ and must run with
    # BASE_PATH=/<key>, e.g.
    #   ["led=http://homerun2-led-catcher.homerun2.svc.cluster.local"]
    services: [str] = []

    # Look of this deployment (internal/branding): a theme preset —
    # "homerun2" (pixel banner) or "machinery" (lab portal) — whose palette
    # the services behind the portal take on too (PORTAL_THEME); optional
    # header text (PORTAL_TITLE, PORTAL_SUBTITLE).
    theme: str = "homerun2"
    title: str = ""
    subtitle: str = ""
    # Header graphic instead of the text: an existing ConfigMap holding the
    # image (.svg, .png, .jpg or .webp, at most 1 MiB) under headerImageKey,
    # e.g. kubectl create configmap lab-portal-branding --from-file=header.svg
    headerImageConfigMap: str = ""
    headerImageKey: str = "header.svg"

    # Login in front of the portal (#24): "none", "local" or "oidc". Both
    # need an existing Secret authSecret with the keys:
    #   users               one "name:bcrypt-hash[:group,group]" per line
    #                       (htpasswd -nbB name password); with oidc the
    #                       emergency access for when the provider is down
    #   session-key         at least 32 random bytes (openssl rand -base64 48)
    #   oidc-client-secret  oidc only; leave the key out for a public client
    #                       (PKCE without secret)
    #   kiosk-token         optional, at least 32 bytes: a kiosk display opens
    #                       /kiosk?…&kiosk_token=<token> and reads without login
    authMode: str = "none"
    authSecret: str = ""
    authSessionTTL: str = "12h"
    # OIDC (Zitadel): issuer and client of the portal's application; the
    # redirect URI defaults to https://<httpRouteHostname>/auth/callback.
    # Zitadel puts the project roles into the ID token under
    # urn:zitadel:iam:org:project:roles (id_token_role_assertion); only users
    # with one of authOidcAllowedGroups get in (empty: every user the
    # provider lets through).
    authOidcIssuer: str = ""
    authOidcClientId: str = ""
    authOidcRedirectUrl: str = ""
    # Empty reads "groups", else urn:zitadel:iam:org:project:roles.
    authOidcGroupsClaim: str = ""
    authOidcAllowedGroups: [str] = []
    authOidcName: str = "Zitadel"
    # A provider certificate from a private PKI: an existing ConfigMap with
    # the CA (PEM) under authOidcCaKey, trusted besides the system CAs, e.g.
    # kubectl create configmap lab-portal-oidc-ca --from-file=ca.crt
    authOidcCaConfigMap: str = ""
    authOidcCaKey: str = "ca.crt"

    # How often and how patiently the services' /health is checked
    healthInterval: str = "10s"
    healthTimeout: str = "2s"

    # Service
    serviceType: str = "ClusterIP"
    servicePort: int = 80
    containerPort: int = 8080

    # HTTPRoute (Gateway API)
    httpRouteEnabled: bool = False
    httpRouteParentRefName: str = ""
    httpRouteParentRefNamespace: str = ""
    httpRouteHostname: str = ""
    httpRouteAnnotations: {str:str} = {}

    # Extra environment variables
    extraEnvVars: {str:str} = {}

    # Labels and annotations
    labels: {str:str} = {}
    annotations: {str:str} = {}

    check:
        replicas >= 0, "replicas must be >= 0"
        servicePort >= 1 and servicePort <= 65535, "servicePort must be valid"
        containerPort >= 1 and containerPort <= 65535, "containerPort must be valid"
        name != "", "name must not be empty"
        theme in ["homerun2", "machinery"], "theme must be homerun2 or machinery"
        authMode in ["none", "local", "oidc"], "authMode must be none, local or oidc"
        authMode == "none" or authSecret, "authMode local or oidc needs authSecret"
        authMode != "oidc" or authOidcIssuer and authOidcClientId, "authMode oidc needs authOidcIssuer and authOidcClientId"
        authMode != "oidc" or authOidcRedirectUrl or httpRouteHostname, "authMode oidc needs authOidcRedirectUrl or httpRouteHostname"
        namespace != "", "namespace must not be empty"
