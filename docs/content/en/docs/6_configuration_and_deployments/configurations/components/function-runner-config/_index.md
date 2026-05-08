---
title: "Function Runner"
type: docs
weight: 3
description: "Configure the Function Runner component"
---

{{% alert title="Note" color="primary" %}}
KPT functions and KRM functions are synonymous terms referring to the same containerized functions.
{{% /alert %}}

The Function Runner executes KRM functions in a secure, isolated environment.

## Function Configuration via FunctionConfig CRD

FunctionConfig CRDs provide declarative configuration for KRM function execution. This is the primary method for configuring functions and replaces the previous ConfigMap-based approach.

The function-runner includes an embedded reconciler that:
- Watches FunctionConfig resources in the configured namespace
- Maintains an internal cache of function configurations
- Uses the cached configuration to determine executor selection and execution parameters

### FunctionConfig Resource Structure

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

### Configuration Fields

#### spec.image
The base function image name without registry prefix or tag. This is matched against function references to determine which configuration to apply.

#### spec.prefixes
List of registry prefixes that should be matched for this function. An empty string matches the default registry prefix. If omitted, only exact image matches are considered.

#### spec.podExecutor
Configuration for running functions as Kubernetes pods:
- **tags**: List of image tags this configuration applies to
- **timeToLive**: Duration pods live before garbage collection (default: 30m)
- **maxParallelExecutions**: Maximum number of concurrent pods for this function
- **preferredMaxQueueLength**: Maximum waitlist length per pod
- **templateOverrides**: Customizations to apply to the pod template

#### spec.binaryExecutor
Configuration for substituting function execution with a local binary:
- **tags**: List of image tags that should use the binary
- **path**: Absolute path or relative path to the functions directory

#### spec.goExecutor
Configuration for native Go function execution within the porch-server:
- **tags**: List of image tags that should use native execution
- **id**: Function identifier for registration (defaults to spec.image if omitted)

### Template Overrides

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

### Example FunctionConfig Resources

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

### RBAC Requirements

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

## Configuration Options

### Command Line Arguments

#### Generic Arguments
```bash
args:
- --port=9445                    # Server port (default: 9445)
- --disable-runtimes=exec,pod     # Disable specific runtimes (exec, pod)
- --log-level=2                   # Log verbosity level 0-5 (default: 2)
```

#### Exec Runtime Arguments
```bash
args:
- --functions=./functions         # Path to cached functions (default: ./functions)
```

{{% alert title="Note" color="primary" %}}
The `--config` flag for exec runtime configuration is deprecated. Use FunctionConfig CRDs instead.
{{% /alert %}}

#### Pod Runtime Arguments
```bash
args:
- --warm-up-pod-cache=true         # Warm up pod cache on startup (default: true)
- --pod-namespace=porch-fn-system  # Namespace for KRM function pods (default: porch-fn-system)
- --pod-ttl=30m                    # Pod TTL before GC (default: 30m)
- --scan-interval=1m               # GC scan interval (default: 1m)
- --max-request-body-size=6291456  # Max gRPC message size in bytes (default: 6MB)
- --max-waitlist-length            # Maximum waitlist length per pod
- --max-parallel-pods-per-function # Maximum parallel pods per function
```

{{% alert title="Note" color="primary" %}}
The `--pod-cache-config` and `--function-pod-template` flags are deprecated. Use FunctionConfig CRDs to configure pod execution settings and template customizations.
{{% /alert %}}

#### Private Registry Arguments
```bash
args:
- --enable-private-registries=false              # Enable private registry support
- --registry-auth-secret-path=/var/tmp/config-secret/.dockerconfigjson  # Registry auth secret path
- --registry-auth-secret-name=auth-secret        # Registry auth secret name
- --enable-private-registries-tls=false          # Enable TLS for private registries
- --tls-secret-path=/var/tmp/tls-secret/         # TLS secret path
```

### Environment Variables

```bash
env:
- name: WRAPPER_SERVER_IMAGE
  value: "<wrapper-server-image>"  # Required for pod runtime
```

## Advanced Configuration

### Pod Templates

Customize function evaluator pod specifications using ConfigMap templates:

```bash
args:
- --function-pod-template=kpt-function-eval-pod-template  # ConfigMap name
```

For detailed pod template configuration, see [Pod Templates]({{% relref "pod-templates" %}}) documentation.

## Runtime Configuration

### Exec Runtime

The exec runtime runs functions as local executables:

```bash
args:
- --functions=./functions         # Directory containing cached function executables
```

Configure function-to-binary mappings using FunctionConfig resources with `binaryExecutor` specification.

### Pod Runtime

The pod runtime runs functions as Kubernetes pods:

```bash
args:
- --pod-namespace=porch-fn-system # Namespace for function pods
- --pod-ttl=30m                   # How long pods live before cleanup
- --scan-interval=1m              # How often to scan for expired pods
- --warm-up-pod-cache=true        # Pre-deploy common function pods
```

Configure pod execution settings, resource limits, and template customizations using FunctionConfig resources with `podExecutor` specification.

### Disabling Runtimes

To disable specific runtimes:

```bash
args:
- --disable-runtimes=exec         # Disable exec runtime only
- --disable-runtimes=pod          # Disable pod runtime only
- --disable-runtimes=exec,pod     # Disable both runtimes
```

## Resource Limits

```bash
resources:
  requests:
    memory: "512Mi"
    cpu: "200m"
  limits:
    memory: "1Gi"
    cpu: "1000m"
```

## Health Checks

```bash
livenessProbe:
  grpc:
    port: 9445
  initialDelaySeconds: 30
  periodSeconds: 10

readinessProbe:
  grpc:
    port: 9445
  initialDelaySeconds: 5
  periodSeconds: 5
```

## Complete Example

Complete Function Runner deployment configuration:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: function-runner
  namespace: porch-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: function-runner
  template:
    metadata:
      labels:
        app: function-runner
    spec:
      containers:
      - name: function-runner
        image: function-runner:latest
        args:
        - --port=9445
        - --log-level=2
        - --pod-namespace=porch-fn-system
        - --pod-ttl=30m
        - --scan-interval=1m
        - --warm-up-pod-cache=true
        - --max-request-body-size=6291456
        env:
        - name: WRAPPER_SERVER_IMAGE
          value: "wrapper-server:latest"
        ports:
        - containerPort: 9445
          protocol: TCP
        resources:
          requests:
            memory: "512Mi"
            cpu: "200m"
          limits:
            memory: "1Gi"
            cpu: "1000m"
        livenessProbe:
          grpc:
            port: 9445
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          grpc:
            port: 9445
          initialDelaySeconds: 5
          periodSeconds: 5
```

{{% alert title="Note" color="primary" %}}
For advanced configuration options:
- [Pod Templates]({{% relref "pod-templates" %}}) - Customize function pod specifications
- [Private Registries]({{% relref "private-registries-config" %}}) - Configure private registry access
{{% /alert %}}
