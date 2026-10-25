"""
Common labels for sthings-portal resources
"""

import schema

# Get command-line options
_name = option("config.name")
_image = option("config.image")
_namespace = option("config.namespace")
_services = option("config.services")
_healthInterval = option("config.healthInterval")
_healthTimeout = option("config.healthTimeout")
_httpRouteEnabled = option("config.httpRouteEnabled")
_httpRouteParentRefName = option("config.httpRouteParentRefName")
_httpRouteParentRefNamespace = option("config.httpRouteParentRefNamespace")
_httpRouteHostname = option("config.httpRouteHostname")
_httpRouteAnnotations = option("config.httpRouteAnnotations") or {}
_extraEnvVars = option("config.extraEnvVars") or {}
_theme = option("config.theme")
_title = option("config.title")
_subtitle = option("config.subtitle")
_headerImageConfigMap = option("config.headerImageConfigMap")
_headerImageKey = option("config.headerImageKey")

# Config instance with command-line overrides
config: schema.Portal = schema.Portal {
    if _name:
        name = _name
    if _theme:
        theme = _theme
    if _title:
        title = _title
    if _subtitle:
        subtitle = _subtitle
    if _headerImageConfigMap:
        headerImageConfigMap = _headerImageConfigMap
    if _headerImageKey:
        headerImageKey = _headerImageKey
    if _image:
        image = _image
    if _namespace:
        namespace = _namespace
    if _services:
        services = _services
    if _healthInterval:
        healthInterval = _healthInterval
    if _healthTimeout:
        healthTimeout = _healthTimeout
    if _httpRouteEnabled:
        httpRouteEnabled = _httpRouteEnabled in [True, "True", "true"]
    if _httpRouteParentRefName:
        httpRouteParentRefName = _httpRouteParentRefName
    if _httpRouteParentRefNamespace:
        httpRouteParentRefNamespace = _httpRouteParentRefNamespace
    if _httpRouteHostname:
        httpRouteHostname = _httpRouteHostname
    if _httpRouteAnnotations:
        httpRouteAnnotations = _httpRouteAnnotations
    if _extraEnvVars:
        extraEnvVars = _extraEnvVars
}

# Common labels applied to all resources
commonLabels = {
    "app.kubernetes.io/name": config.name
    "app.kubernetes.io/instance": config.name
    "app.kubernetes.io/component": "portal"
    "app.kubernetes.io/part-of": "homerun2"
    **config.labels
}

# Selector labels for pods
selectorLabels = {
    "app.kubernetes.io/name": config.name
    "app.kubernetes.io/instance": config.name
}
