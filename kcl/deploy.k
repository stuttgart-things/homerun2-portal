"""
Deployment for sthings-portal
"""

import labels

config = labels.config

deployment = {
    apiVersion: "apps/v1"
    kind: "Deployment"
    metadata: {
        name: config.name
        namespace: config.namespace
        labels: labels.commonLabels
    }
    spec: {
        replicas: config.replicas
        selector: {
            matchLabels: labels.selectorLabels
        }
        strategy: {
            type: "RollingUpdate"
            rollingUpdate: {
                maxSurge: 1
                maxUnavailable: 0
            }
        }
        template: {
            metadata: {
                labels: labels.commonLabels
            }
            spec: {
                serviceAccountName: config.name
                containers: [
                    {
                        name: config.name
                        image: config.image
                        imagePullPolicy: config.imagePullPolicy
                        ports: [
                            {
                                name: "http"
                                containerPort: config.containerPort
                                protocol: "TCP"
                            }
                        ]
                        env: [
                            {
                                name: "PORT"
                                value: str(config.containerPort)
                            }
                        ] + ([
                            {
                                name: "AUTH_SESSION_KEY"
                                valueFrom: {
                                    secretKeyRef: {
                                        name: config.authSecret
                                        key: "session-key"
                                    }
                                }
                            }
                        ] if config.authMode != "none" else [])
                        envFrom: [
                            {
                                configMapRef: {
                                    name: "{}-config".format(config.name)
                                }
                            }
                        ]
                        resources: {
                            requests: {
                                cpu: config.cpuRequest
                                memory: config.memoryRequest
                            }
                            limits: {
                                cpu: config.cpuLimit
                                memory: config.memoryLimit
                            }
                        }
                        # /health reports the portal alone: a service behind it
                        # being down must not restart the portal.
                        livenessProbe: {
                            httpGet: {
                                path: "/health"
                                port: "http"
                            }
                            initialDelaySeconds: 5
                            periodSeconds: 10
                            timeoutSeconds: 5
                            failureThreshold: 3
                        }
                        readinessProbe: {
                            httpGet: {
                                path: "/health"
                                port: "http"
                            }
                            initialDelaySeconds: 2
                            periodSeconds: 5
                            timeoutSeconds: 3
                            failureThreshold: 3
                        }
                        if config.headerImageConfigMap or config.authMode != "none":
                            volumeMounts: ([
                                {
                                    name: "branding"
                                    mountPath: "/etc/portal/branding"
                                    readOnly: True
                                }
                            ] if config.headerImageConfigMap else []) + ([
                                {
                                    name: "auth"
                                    mountPath: "/etc/portal/auth"
                                    readOnly: True
                                }
                            ] if config.authMode != "none" else [])
                        securityContext: {
                            readOnlyRootFilesystem: True
                            runAsNonRoot: True
                            allowPrivilegeEscalation: False
                            capabilities: {
                                drop: ["ALL"]
                            }
                        }
                    }
                ]
                if config.headerImageConfigMap or config.authMode != "none":
                    volumes: ([
                        {
                            name: "branding"
                            configMap: {
                                name: config.headerImageConfigMap
                            }
                        }
                    ] if config.headerImageConfigMap else []) + ([
                        {
                            name: "auth"
                            secret: {
                                secretName: config.authSecret
                                items: [{key: "users", path: "users"}]
                            }
                        }
                    ] if config.authMode != "none" else [])
                securityContext: {
                    runAsNonRoot: True
                    runAsUser: 65532
                    runAsGroup: 65532
                    fsGroup: 65532
                    seccompProfile: {
                        type: "RuntimeDefault"
                    }
                }
            }
        }
    }
}
