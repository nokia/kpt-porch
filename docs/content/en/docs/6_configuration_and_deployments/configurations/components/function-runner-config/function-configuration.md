---
title: "Function Configuration"
type: docs
weight: 2
description: "Customize function evaluation using FunctionConfig resources"
---

## Overview

FunctionConfig CRs provide declarative configuration for KRM function execution. This is the primary method for configuring functions and replaces the previous ConfigMap-based approach.

The function-runner includes an embedded reconciler that:
- Watches FunctionConfig resources in the configured namespace
- Maintains an internal cache of function configurations
- Uses the cached configuration to determine executor selection and execution parameters

## FunctionConfig Resource Structure

```yaml
apiVersion: config.porch.kpt.dev/v1alpha1
kind: FunctionConfig
metadata:
  name: <function-name>
  namespace: porch-fn-system
spec:
  # Base function image name (without registry prefix or tag)
  image: <image-name>
  
  # Optional: List of registry prefixes that should be matched
  # Empty string or omitted uses the default prefix
  prefixes:
    - ""
    - ghcr.io/kptdev/krm-functions-catalog
  
  # Optional: Configuration for pod-based execution
  podExecutor:
    tags:
      - v1.0.0
    timeToLive: 30m
    maxParallelExecutions: 2
    preferredMaxQueueLength: 1
    templateOverrides:
      serviceAccountName: custom-sa
      securityContext:
        runAsUser: 1000
      initContainer:
        resources:
          limits:
            memory: "256Mi"
      container:
        resources:
          limits:
            memory: "512Mi"
  
  # Optional: Configuration for binary executable substitution
  binaryExecutor:
    tags:
      - v1.0.0
    path: /path/to/binary  # Absolute or relative to functions directory
  
  # Optional: Configuration for native Go execution
  goExecutor:
    tags:
      - v1.0
    id: custom-function-id  # Optional: defaults to spec.image
```

## Configuration Fields

### spec.image
The base function image name without registry prefix or tag. This is matched against function references to determine which configuration to apply.

### spec.prefixes
List of registry prefixes that should be matched for this function. An empty string matches the default registry prefix. If omitted, only exact image matches are considered.

### spec.podExecutor
Configuration for running functions as Kubernetes pods:
- **tags**: List of image tags this configuration applies to
- **timeToLive**: Duration pods live before garbage collection (default: 30m)
- **maxParallelExecutions**: Maximum number of concurrent pods for this function
- **preferredMaxQueueLength**: Maximum waitlist length per pod
- **templateOverrides**: Customizations to apply to the pod template

### spec.binaryExecutor
Configuration for substituting function execution with a local binary in the function-runner pod:
- **tags**: List of image tags that should use the binary
- **path**: Absolute path or relative path to the functions directory

### spec.goExecutor
Configuration for native Go function execution within the porch-server:
- **tags**: List of image tags that should use native execution
- **id**: Function identifier for registration (defaults to spec.image if omitted)

## Template Overrides

The `templateOverrides` structure allows customization of pod execution:

```yaml
templateOverrides:
  # Service account for the function pod
  serviceAccountName: function-sa
  
  # Pod security context
  securityContext:
    runAsUser: 1000
    runAsGroup: 1000
    fsGroup: 1000
  
  # Init container customizations
  initContainer:
    resources:
      limits:
        memory: "256Mi"
        cpu: "500m"
      requests:
        memory: "128Mi"
        cpu: "250m"
    env:
      - name: CUSTOM_VAR
        value: "value"
    envFrom:
      - secretRef:
          name: function-secret
  
  # Main container customizations
  container:
    resources:
      limits:
        memory: "512Mi"
        cpu: "1000m"
      requests:
        memory: "256Mi"
        cpu: "500m"
    env:
      - name: CUSTOM_VAR
        value: "value"
    envFrom:
      - configMapRef:
          name: function-config
```

## Example FunctionConfig Resources

Basic pod executor configuration:

```yaml
apiVersion: config.porch.kpt.dev/v1alpha1
kind: FunctionConfig
metadata:
  name: set-namespace
  namespace: porch-fn-system
spec:
  image: set-namespace
  prefixes:
    - ""
    - ghcr.io/kptdev/krm-functions-catalog
  podExecutor:
    tags:
      - v0.4.1
    timeToLive: 30m
```

Multi-executor configuration:

```yaml
apiVersion: config.porch.kpt.dev/v1alpha1
kind: FunctionConfig
metadata:
  name: starlark
  namespace: porch-fn-system
spec:
  image: starlark
  prefixes:
    - ""
    - ghcr.io/kptdev/krm-functions-catalog
  podExecutor:
    tags:
      - v0.4.3
    timeToLive: 30m
  binaryExecutor:
    tags:
      - v0.5.2
    path: starlark
  goExecutor:
    id: starlark
    tags:
      - v0.5
      - v0.5.5
```

Configuration with resource limits:

```yaml
apiVersion: config.porch.kpt.dev/v1alpha1
kind: FunctionConfig
metadata:
  name: gatekeeper
  namespace: porch-fn-system
spec:
  image: gatekeeper
  prefixes:
    - ""
    - ghcr.io/kptdev/krm-functions-catalog
  podExecutor:
    tags:
      - v0.2.1
    timeToLive: 30m
    maxParallelExecutions: 3
    preferredMaxQueueLength: 2
    templateOverrides:
      container:
        resources:
          limits:
            memory: "1Gi"
            cpu: "1000m"
          requests:
            memory: "512Mi"
            cpu: "500m"
```

## RBAC Requirements

The function-runner requires permissions to watch FunctionConfig resources:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: function-runner
  namespace: porch-fn-system
rules:
  - apiGroups: ["config.porch.kpt.dev"]
    resources: ["functionconfigs"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["config.porch.kpt.dev"]
    resources: ["functionconfigs/status"]
    verbs: ["get", "update", "patch"]
```
