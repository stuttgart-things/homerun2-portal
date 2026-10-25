[package]
name = "deploy-sthings-portal"
version = "0.1.0"
description = "KCL module for deploying sthings-portal on Kubernetes"

[dependencies]
k8s = "1.31"

[profile]
entries = [
    "main.k"
]
