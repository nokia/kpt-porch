---
title: "Function Runner Interactions"
type: docs
weight: 4
description: |
  How the function runner integrates with Porch and external systems.
---

## Overview

The Function Runner is a **separate gRPC service** that interacts with multiple systems: the Task Handler (via gRPC), Kubernetes API (for pod management and FunctionConfig CRDs), container registries (for image metadata), and wrapper servers (for function execution). It operates independently from the Porch server, enabling isolated function execution.

The Function Runner includes an **embedded FunctionConfig reconciler** that watches FunctionConfig CRDs and populates an internal cache. This cache determines which executor (pod, binary, or Go) to use for each function image.

### High-Level Architecture

```
┌──────────────────────────────────────────────────────────────┐
│              Function Runner Service                         │
│                                                              │
│  ┌──────────────┐      ┌──────────────┐      ┌────────────┐  │
│  │ Task Handler │ ───> │    gRPC      │ ───> │ Evaluators │  │
│  │  (in Porch)  │      │   Server     │      │            │  │
│  └──────────────┘      └──────────────┘      └────────────┘  │
│         ↑                      │                  │          │
│         │                      ↓                  ↓          │
│         │              ┌───────────────┐     ┌──────────┐    │
│         │              │ FunctionConfig│     │ Executor │    │
│         │              │  Reconciler   │ ──> │  Cache   │    │
│         │              └───────────────┘     └──────────┘    │
│         │                      │                  │          │
│         │                      ↓                  ↓          │
│         │              ┌──────────────┐      ┌─────────┐     │
│         └──────────────│  Kubernetes  │      │  Image  │     │
│                        │     API      │      │  Cache  │     │
│                        └──────────────┘      └─────────┘     │
│                               │                   │          │
│                               ↓                   ↓          │
│                        ┌──────────────┐     ┌──────────┐     │
│                        │ Function Pods│     │ Registry │     │
│                        │  + Services  │     │   APIs   │     │
│                        └──────────────┘     └──────────┘     │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

External Resources:
- FunctionConfig CRDs (define executor configurations)
- ServiceTemplate CRDs (define pod/service templates)

## FunctionConfig Reconciler and Cache

The Function Runner includes an embedded reconciler that watches FunctionConfig CRDs and maintains an internal cache of executor configurations.

### Reconciliation Flow

```
FunctionConfig CRD
        ↓
  Watch Event
        ↓
  Reconciler
        ↓
  Parse Spec
        ↓
  ┌────┴────┬──────────┬──────────┐
  ↓         ↓          ↓          ↓
Pod     Binary      Go        Update
Config  Config    Config     Status
  ↓         ↓          ↓          ↓
Pod     Binary      Go        Return
Cache   Cache      Cache
  ↓         ↓          ↓
  └────┬────┴──────────┘
       ↓
  Ready for
  Evaluation
```

**Reconciler responsibilities:**
- Watch FunctionConfig resources across all namespaces
- Parse executor configurations (pod, binary, Go)
- Populate executor-specific caches
- Update FunctionConfig status with observed generation
- Handle configuration errors and report via status

**Cache structure:**
- **Pod executor cache**: Maps image names (with tags/prefixes) to `PodExecutorConfig` (TTL, parallelism, template overrides)
- **Binary executor cache**: Maps image names (with tags) to local binary paths
- **Go executor cache**: Maps image names (with tags) to in-process Go function processors

### Executor Selection Logic

When evaluating a function, the executor selection follows this pattern:

```
EvaluateFunction Request
        ↓
  Extract Image Name
        ↓
  Query Executor Cache
        ↓
  ┌────┴────┬──────────┬──────────┐
  ↓         ↓          ↓          ↓
Go      Binary     Pod       Not Found
Match   Match     Match       Error
  ↓         ↓          ↓          ↓
Go      Binary     Pod       Fallback
Exec    Exec      Exec      to Next
```

**Selection characteristics:**
- Image name extracted from request (including registry prefix and tag)
- Cache lookup considers full image reference with version
- Single function image can have multiple executors for different versions
  - Example: `apply-replacements:v0.1.0` → binary executor
  - Example: `apply-replacements:v0.2.0` → pod executor
- First matching executor is used
- NotFoundError triggers fallback to next evaluator in chain

### Configuration Mapping

FunctionConfig resources map to internal cache entries:

**PodExecutor → Pod Cache:**
- `spec.image` + `spec.prefixes[]` + `spec.podExecutor.tags[]` → full image references
- `spec.podExecutor.timeToLive` → pod TTL before garbage collection
- `spec.podExecutor.maxParallelExecutions` → concurrent pod limit per function
- `spec.podExecutor.preferredMaxQueueLength` → waitlist length before scaling
- `spec.podExecutor.templateOverrides` → pod/container customizations

**BinaryExecutor → Binary Cache:**
- `spec.image` + `spec.binaryExecutor.tags[]` → image references
- `spec.binaryExecutor.path` → absolute or relative binary path
- Path resolution: absolute if starts with `/`, else relative to `--functions` dir

**GoExecutor → Go Cache:**
- `spec.image` + `spec.goExecutor.tags[]` → image references
- `spec.goExecutor.id` → internal function registration ID (defaults to `spec.image`)
- Registered Go functions: `apply-replacements`, `set-namespace`, `starlark`

## Task Handler Integration

The Function Runner integrates with the Task Handler through the gRPC Runtime:

### gRPC Runtime Pattern

```
Task Handler
        ↓
  gRPC Runtime (in Engine)
        ↓
  Persistent Connection
        ↓
Function Runner Service
        ↓
  Evaluator Selection
        ↓
  Function Execution
```

**Integration characteristics:**
- Task Handler uses gRPC Runtime to communicate with Function Runner
- Single persistent connection shared across all function executions
- Connection established at Porch startup, closed on shutdown
- Function Runner address configured via `--function-runner-address` flag

**Request-response flow:**
- Task Handler serializes ResourceList as YAML
- gRPC Runtime sends EvaluateFunctionRequest
- Function Runner selects appropriate evaluator (exec or pod)
- Response includes transformed ResourceList and function logs
- NotFoundError triggers fallback to next evaluator in multi-evaluator chain

### Render Pipeline Invocation

The Task Handler invokes Function Runner during render operations:

```
Task Handler
        ↓
  Execute Render Task
        ↓
  For Each Function in Kptfile:
        ↓
    Call EvaluateFunction
        ↓
    Function Runner Executes
        ↓
    Return Transformed Resources
        ↓
  Continue Pipeline
```

**Pipeline characteristics:**
- Sequential execution (one function at a time)
- Each function sees output of previous function
- First error stops pipeline
- Results aggregated into RenderStatus by Task Handler

### Message Size Configuration

Both Porch and Function Runner must agree on message size limits:

**Configuration synchronization:**
- Porch server: `--function-runner-grpc-max-message-size` flag
- Function Runner: `--max-request-body-size` flag
- Both must be set to same value (default: 6MB)
- Mismatched limits cause cryptic gRPC errors

**Why coordination matters:**
- Large packages with many resources exceed default gRPC limits
- ResourceList serialized as YAML can be large
- Must be coordinated during deployment

## Evaluator Execution Patterns

The Function Runner uses different execution patterns based on evaluator type. Executor selection is determined by the FunctionConfig cache, which maps function images (with prefixes and tags) to executor configurations.

### Pod-Based Execution

```
gRPC Request
        ↓
  Pod Evaluator
        ↓
  Check Pod Cache
        ↓
  ┌────┴────┬─────────┐
  ↓         ↓         ↓
Cache    Cache     Cache
Hit      Miss      Invalid
  ↓         ↓         ↓
Reuse    Create    Recreate
Pod      Pod       Pod
  ↓         ↓         ↓
  └────┬────┴─────────┘
       ↓
  Connect via Service
       ↓
  Call Wrapper Server
       ↓
  Return Response
```

**Execution pattern:**
- Pod executor configuration comes from FunctionConfig resources
- Pod cache checked for existing pod (reuse if available)
- Pod selection uses round-robin among pods with minimum waitlist length
- Cache miss triggers pod creation with wrapper server
- ClusterIP service provides stable DNS-based access
- gRPC connection to wrapper server in pod
- Wrapper server executes function binary and returns results
- Multiple evaluations can execute in parallel on the same pod

**Configuration from FunctionConfig:**
- `podExecutor.timeToLive`: How long pods remain cached before garbage collection
- `podExecutor.maxParallelExecutions`: Maximum concurrent pods per function
- `podExecutor.preferredMaxQueueLength`: Waitlist length before scaling up
- `podExecutor.templateOverrides`: Pod/container customizations (resources, env, security)
- `podExecutor.tags[]`: Image tags this configuration applies to

**Template system:**
- ServiceTemplate CRDs define pod and service templates
- Base templates: `base-pod-template` and `base-service-template` in function runner namespace
- Inline templates as fallback if CRDs not found
- Template overrides from FunctionConfig merged with base templates
- Template version tracked via `resourceVersion` for pod replacement on changes

**For detailed pod lifecycle, see [Pod Lifecycle Management]({{% relref "/docs/5_architecture_and_components/function-runner/functionality/pod-lifecycle-management.md" %}}).**

### Executable-Based Execution

```
gRPC Request
        ↓
  Executable Evaluator
        ↓
  Lookup in Cache
        ↓
  Found? ──No──> Return NotFoundError
        │
       Yes
        ↓
  Execute Binary Locally
        ↓
  Return Response
```

**Execution pattern:**
- Binary executor configuration comes from FunctionConfig resources
- Cache maps image names (with tags) to local binary paths
- Fast O(1) lookup by image name
- Direct process execution with ResourceList input
- NotFoundError triggers fallback in multi-evaluator

**Configuration from FunctionConfig:**
- `binaryExecutor.tags[]`: Image tags mapped to this binary
- `binaryExecutor.path`: Binary path (absolute or relative to `--functions` dir)
- Multiple tags can map to same binary (e.g., `v0.1.0`, `v0.1`, specific digest)

**For detailed function evaluation, see [Function Evaluation]({{% relref "/docs/5_architecture_and_components/function-runner/functionality/function-evaluation.md" %}}).**

### Go-Based Execution

```
gRPC Request
        ↓
  Go Evaluator (in porch-server)
        ↓
  Lookup in Cache
        ↓
  Found? ──No──> Return NotFoundError
        │
       Yes
        ↓
  Call Go Function Processor
        ↓
  Return Response
```

**Execution pattern:**
- Go executor provides highest performance by avoiding process/container overhead
- Functions executed as native Go function calls within porch-server process
- Cache maps image names to `ResourceListProcessor` implementations
- Direct in-process execution with ResourceList input
- NotFoundError triggers fallback to next evaluator (typically gRPC runtime)

**Configuration from FunctionConfig:**
- `goExecutor.tags[]`: Image tags mapped to this Go function
- `goExecutor.id`: Internal registration ID (defaults to `spec.image`)
- Registered functions: `apply-replacements`, `set-namespace`, `starlark`

**Benefits:**
- No container startup overhead
- No gRPC network calls
- Fastest possible execution for compatible functions
- Lower resource usage compared to pod-based execution

**For detailed function evaluation, see [Function Evaluation]({{% relref "/docs/5_architecture_and_components/function-runner/functionality/function-evaluation.md" %}}).**

## Kubernetes API Integration

The Function Runner uses Kubernetes API for pod and service management:

### Resource Management Pattern

```
Pod Evaluator
        ↓
  Kubernetes Client
        ↓
  ┌──────┴──────┬──────────┬─────────┐
  ↓             ↓          ↓         ↓
Pod Ops    Service Ops    CRs     Secrets
  ↓             ↓          ↓         ↓
Create/Get  Create/Get   Function   Auth
Delete      Delete       Config     Config
```

**Resource operations:**
- **Pods**: Create from template, get status, list by label, delete
- **Services**: Create ClusterIP frontend, get endpoints, delete
- **FunctionConfig CRs**: Watch for configuration changes
- **ServiceTemplate CRs**: Base pod and service templates
- **PodTemplate resources**: Base pod specifications
- **Secrets**: Access registry authentication and TLS certificates

**Template system:**
- ServiceTemplate CRs define base pod and service templates
- PodTemplate resources provide base pod specifications
- Inline templates as fallback if CRs/resources not found
- FunctionConfig provides per-function template overrides
- Template version tracked for pod replacement on changes

**For detailed pod management, see [Pod Lifecycle Management]({{% relref "/docs/5_architecture_and_components/function-runner/functionality/pod-lifecycle-management.md" %}}).**

### Service Mesh Compatibility

Each function pod has a dedicated ClusterIP service:

**Service frontend pattern:**
- ClusterIP service created for each pod
- Selector matches pod's image label
- Port 9446 targeting wrapper server
- Provides stable DNS name for service mesh sidecars
- Deleted when pod is garbage collected

**Benefits:**
- Service mesh sidecars require stable DNS names
- Services provide consistent endpoint for pod communication
- Enables service mesh features (mTLS, traffic management, observability)

## Container Registry Integration

The Function Runner integrates with container registries for image metadata and authentication:

### Image Metadata Caching

```
Pod Creation
        ↓
  Check Image Cache
        ↓
  Cache Hit? ──Yes──> Use Cached Metadata
        │
        No
        ↓
  Connect to Registry
        ↓
  Fetch Manifest + Config
        ↓
  Extract Digest + Entrypoint
        ↓
  Store in Cache
        ↓
  Return Metadata
```

**Caching pattern:**
- In-memory cache of image digests and entrypoints
- Avoids repeated registry API calls
- Faster pod creation (no digest resolution delay)
- Cache persists for Function Runner lifetime

**For detailed image management, see [Image and Registry Management]({{% relref "/docs/5_architecture_and_components/function-runner/functionality/image-registry-management.md" %}}).**

### Authentication and TLS

The Function Runner supports private registries with authentication:

**Authentication flow:**
- Default registry (gcr.io/kpt-fn/) uses default keychain
- Custom registries use Docker config JSON format
- Auth secret mounted into Function Runner pod
- Image pull secrets created and attached to function pods

**TLS configuration:**
- Custom CA certificates for secure registry connections
- TLS secret contains ca.crt or ca.pem
- Custom transport created with TLS config
- Fallback to default transport on TLS errors

**Configuration:**
- `--registry-auth-secret-path`: Path to Docker config JSON
- `--enable-private-registries`: Enable private registry support
- `--tls-secret-path`: Path to TLS certificates

## Wrapper Server Integration

The wrapper server provides gRPC interface for function execution inside pods:

### Wrapper Server Pattern

```
Pod Evaluator
        ↓
  Connect to Service
        ↓
  Call EvaluateFunction
        ↓
Wrapper Server (in pod)
        ↓
  Execute Function Binary
        ↓
  • Stdin: ResourceList
  • Stdout: Transformed ResourceList
  • Stderr: Function logs
        ↓
  Parse Output
        ↓
  Return Response
```

**Integration pattern:**
- Init container copies wrapper-server binary to shared volume
- Main container executes wrapper-server as entrypoint
- Wrapper server receives gRPC requests on port 9446
- Executes original function entrypoint with ResourceList input
- Parses function output and returns structured results

**Communication protocol:**
- ResourceList exchanged as serialized YAML
- Standard kpt ResourceList format
- Function reads from stdin, writes to stdout
- Stderr captured for logging

**Error handling:**
- Non-zero exit codes indicate function failure
- Invalid output triggers parse errors
- Timeout errors if function exceeds deadline
- All errors propagated through gRPC to Task Handler

## Integration Patterns

The Function Runner follows standard integration patterns:

### Separation of Concerns

**Function Runner responsibilities:**
- gRPC service for function execution
- Evaluator selection and orchestration
- Pod and service lifecycle management
- Image metadata caching
- Registry authentication
- FunctionConfig reconciliation and caching

**Task Handler responsibilities:**
- Render pipeline orchestration
- Function sequencing
- Result aggregation
- RenderStatus creation

**Clear boundaries:**
- Function Runner doesn't know about package structure
- Task Handler doesn't know about pod management
- Integration through well-defined gRPC interface

### Stateless Service

**Stateless characteristics:**
- No persistent state between requests
- Pod cache is ephemeral (TTL-based)
- Image cache is in-memory only
- Executor configuration cache populated from FunctionConfig CRs
- Evaluators are stateless

**Benefits:**
- Horizontal scaling possible
- Restart-safe (no state loss)
- Simple deployment model

### Asynchronous Operations

**Async patterns:**
- Pod creation happens in background goroutines
- Garbage collection runs periodically
- TTL updates are asynchronous patches
- Cache warming at startup is concurrent
- FunctionConfig reconciliation is event-driven

**Synchronous patterns:**
- Function execution is synchronous (blocks until complete)
- gRPC requests are synchronous
- Pod readiness polling is synchronous

## Error Handling

The Function Runner handles errors at multiple integration points:

### gRPC Error Propagation

```
Function Execution Error
        ↓
Wrapper Server
        ↓
  gRPC Error
        ↓
Pod Evaluator
        ↓
  Propagate Error
        ↓
Task Handler
        ↓
  Include in RenderStatus
        ↓
Engine
        ↓
  Return to Client
```

**Error types:**
- **NotFoundError**: Function not available (triggers fallback)
- **Execution error**: Function failed (non-zero exit code)
- **Timeout error**: Execution exceeded deadline
- **System error**: Infrastructure problem (pod creation, registry access)

**Error handling strategy:**
- NotFoundError triggers fallback to next evaluator
- Other errors returned immediately without fallback
- Errors include context and details for debugging
- Task Handler aggregates errors into RenderStatus

### Retry and Fallback

**Retry mechanisms:**
- Registry authentication retries on 401
- Pod creation retries on transient errors
- Connection retries on network failures

**Fallback mechanisms:**
- Multi-evaluator fallback on NotFoundError
- TLS fallback on certificate errors
- Default keychain fallback for default registry

## Concurrency and Safety

The Function Runner handles concurrent operations safely:

### Concurrent Execution

**Concurrency characteristics:**
- Multiple function executions can run concurrently
- Multiple evaluations can run in parallel on the same pod
- Each request gets own gRPC connection to pod
- Pod cache manager coordinates access via channels
- Waitlist prevents duplicate pod creation

**Thread safety:**
- Pod cache manager runs in single goroutine (no races)
- Channel-based communication eliminates shared memory
- Image cache uses sync.Map for concurrent access
- Kubernetes client is thread-safe

### Resource Isolation

**Isolation guarantees:**
- Each function pod is isolated
- Functions execute in separate containers
- Resource limits enforced by Kubernetes
- No shared state between function executions

**For detailed concurrency patterns, see [Pod Lifecycle Management]({{% relref "/docs/5_architecture_and_components/function-runner/functionality/pod-lifecycle-management.md" %}}).**
