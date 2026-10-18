[package]
name = "deploy-homerun2-portal"
version = "0.1.0"
description = "KCL module for deploying homerun2-portal on Kubernetes"

[dependencies]
k8s = "1.31"

[profile]
entries = [
    "main.k"
]
