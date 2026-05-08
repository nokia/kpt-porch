---
title: "Function Evaluation"
type: docs
weight: 1
description: |
  Detailed architecture of function evaluation strategies and execution patterns.
---

## Overview

Function evaluation is the core responsibility of the Function Runner - executing KRM (Kubernetes Resource Model) functions through pluggable evaluator strategies. The system uses a strategy pattern where different evaluators handle function execution in different ways (pod-based, executable, or chained), all conforming to a common interface. Configuration is managed via FunctionConfig CRDs that define image matching rules and executor-specific settings.

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────┐
│           Function Evaluation System                    │
│                                                         │
│  ┌──────────────────┐      ┌──────────────────┐         │
│  │  FunctionConfig  │      │   Execution      │         │
│  │      CRDs        │ ───> │   Strategies     │         │
│  │                  │      │                  │         │
│  │  • Image Match   │      │  • Pod Evaluator │         │
│  │  • Executor Type │      │  • Exec Evaluator│         │
│  │  • Configuration │      │  • Multi Eval    │         │
│  └────────┬─────────┘      └──────────────────┘         │
│           │                         │                   │
│           ↓                         │                   │
│  ┌──────────────────┐               │                   │
│  │   Reconciler     │               │                   │
│  │                  │               │                   │
│  │  • Watch CRDs    │               │                   │
│  │  • Populate      │               │                   │
│  │    Cache         │               │                   │
│  └────────┬─────────┘               │                   │
│           │                         │                   │
│           └────────┬────────────────┘                   │
│                    ↓                                    │
│          ┌──────────────────┐                           │
│          │    Wrapper       │                           │
│          │    Server        │                           │
│          │                  │                           │
│          │  • gRPC Frontend │                           │
│          │  • Binary Exec   │                           │
│          │  • Result Parse  │                           │
│          └──────────────────┘                           │
└─────────────────────────────────────────────────────────┘
```

## Evaluator Interface

All evaluators implement a common interface that defines the contract for function execution.

### Interface Contract

**Single operation:**
- **EvaluateFunction**: Accepts context and request, returns response or error

**Request structure:**
- **Image**: Function container image identifier
- **ResourceList**: Serialized KRM resources as YAML bytes

**Response structure:**
- **ResourceList**: Transformed KRM resources as YAML bytes
- **Log**: Function stderr output as bytes

**Contract characteristics:**
- **Synchronous**: Blocks until function execution completes
- **Context-aware**: Respects cancellation and deadlines from context
- **Stateless**: No state maintained between calls
- **Error-typed**: Returns NotFoundError for missing functions to enable fallback

### Evaluator Implementations

Three evaluator implementations provide different execution strategies:

**Pod Evaluator:**
- Executes functions in Kubernetes pods
- Uses wrapper server for gRPC interface
- Manages pod cache with TTL-based expiration
- Handles service mesh compatibility via ClusterIP services
- Configuration driven by FunctionConfig PodExecutor settings

**Executable Evaluator:**
- Executes pre-cached function binaries locally
- FunctionConfig CRDs map images to binary paths
- Fast execution without pod overhead
- Returns NotFoundError for uncached functions
- Configuration driven by FunctionConfig BinaryExecutor settings

**Multi-Evaluator:**
- Chains multiple evaluators together
- Tries each evaluator in sequence
- Falls back on NotFoundError
- Returns first successful response

## Pod Evaluator

Executes functions in Kubernetes pods with caching and lifecycle management. Pod configuration is now driven by FunctionConfig CRDs.

### FunctionConfig-Based Pod Configuration

The pod evaluator uses FunctionConfig CRDs to determine pod execution parameters:

**Configuration structure:**
- FunctionConfig CRDs define pod execution settings
- PodExecutor section specifies TTL, templates, and resources
- Multiple tags can share pod configuration
- Template overrides customize pod and container specs

**Pod configuration options:**
- **TimeToLive**: Duration pods remain cached before cleanup
- **TemplateOverrides**: Customize pod and container settings
  - ServiceAccountName: Override service account
  - SecurityContext: Pod security settings
  - Container resources, env, envFrom: Container customizations
  - InitContainer resources, env, envFrom: Init container customizations

**Example FunctionConfig for pod executor:**

```yaml
apiVersion: config.porch.kpt.dev/v1alpha1
kind: FunctionConfig
metadata:
  name: apply-setters
  namespace: porch-fn-system
spec:
  image: apply-setters
  prefixes:
    - ""
    - ghcr.io/kptdev/krm-functions-catalog
  podExecutor:
    tags:
      - v0.2.2
    timeToLive: 30m
    templateOverrides:
      container:
        resources:
          limits:
            memory: 512Mi
          requests:
            memory: 256Mi
```

### Channel-Based Communication

The pod evaluator uses channels for communication with the pod cache manager:

**Communication pattern:**
- Request sent via channel to pod cache manager
- Blocks waiting for gRPC client from cache manager
- Cache manager handles pod creation/reuse
- Direct gRPC call to wrapper server in pod

**Channel characteristics:**
- **Buffered channel**: Size 1 to prevent blocking
- **One-way communication**: Request sent, client received
- **Synchronization point**: Blocks until client available
- **Error propagation**: Errors sent through same channel

**Benefits:**
- Decouples evaluation from pod management
- Single goroutine manages pod cache (no race conditions)
- Natural backpressure when pods unavailable
- Clean separation of concerns

### Client Connection Management

The pod cache manager maintains gRPC connections to function pods:

**Cache structure:**
- Map from image name to pod and gRPC client
- Includes pod object key for validation
- Stores gRPC client connection for reuse

**Connection validation:**
- Pod still exists (not deleted externally)
- Pod not in Failed state
- Service still exists for pod
- gRPC client target matches service URL
- Pod not being deleted (DeletionTimestamp nil)

**Failed pod handling:**
- Immediately delete failed pods
- Evict from cache
- Trigger new pod creation
- Prevents reusing broken pods

### Waitlist Mechanism

Prevents duplicate pod creation when multiple requests arrive for the same function:

**Waitlist pattern:**
- Multiple requests for same image queue up
- Single pod creation serves all waiters
- Batch notification when pod ready
- Prevents duplicate pod creation

**Error handling:**
- Pod creation errors sent to all waiters
- Waitlist cleared on error
- Each waiter receives error independently
- Allows retry on next request

### Function Execution

Once gRPC client acquired, function execution proceeds:

**Execution characteristics:**
- Synchronous gRPC call
- Context passed through for cancellation
- Timeout enforced by context deadline
- Stderr logged even on success
- Detailed logging on errors

## Executable Evaluator

Executes pre-cached function binaries locally for fast execution.

### Configuration-Based Caching

The executable evaluator uses FunctionConfig CRDs to map images to binaries:

**Configuration structure:**
- FunctionConfig CRDs define image matching rules
- Each FunctionConfig specifies image name, prefixes, and tags
- BinaryExecutor section maps specific tags to local binary paths
- Embedded reconciler watches FunctionConfigs and populates internal cache

**Configuration benefits:**
- Explicit control over cached functions
- No automatic caching (predictable behavior)
- Dynamic updates via Kubernetes API
- Cache automatically syncs with CRD changes

### Function Cache Lookup

**Lookup characteristics:**
- Cache queried with full image reference
- Best matching FunctionConfig entry selected
- Fast O(1) operation
- NotFoundError triggers fallback in multi-evaluator
- No network or Kubernetes API calls during lookup

### FunctionConfig Reconciler

The function-runner includes an embedded FunctionConfig reconciler:

**Reconciler responsibilities:**
- Watches FunctionConfig CRDs in function namespace
- Populates internal cache on FunctionConfig changes
- Maps image references to executor configurations
- Updates status to indicate successful application

**Cache structure:**
- Map from full image reference to binary path
- Includes image prefix resolution
- Handles multiple tags per function
- Prevents duplicate image registrations

### Local Execution

Binary execution happens in-process:

**Execution characteristics:**
- Direct process execution (no container)
- ResourceList passed via stdin
- Output captured from stdout
- Stderr captured for logging
- Context-aware (respects cancellation)

**Performance benefits:**
- No pod startup latency
- No image pull time
- No Kubernetes API overhead
- Millisecond execution times
- Predictable performance

**Example FunctionConfig for binary executor:**

```yaml
apiVersion: config.porch.kpt.dev/v1alpha1
kind: FunctionConfig
metadata:
  name: apply-setters
  namespace: porch-fn-system
spec:
  image: apply-setters
  prefixes:
    - ""
    - ghcr.io/kptdev/krm-functions-catalog
  binaryExecutor:
    tags:
      - v0.2.0
    path: apply-setters
```

This configuration maps `apply-setters:v0.2.0` to the local binary at `./functions/apply-setters`.

## Multi-Evaluator

Chains multiple evaluators with fallback logic.

### Evaluator Chaining

**Chaining characteristics:**
- Sequential evaluation (not parallel)
- First success wins
- Only NotFoundError triggers fallback
- Other errors returned immediately
- Preserves error semantics

**Typical chain:**
1. **Executable evaluator** (fast path)
2. **Pod evaluator** (fallback)

### Fallback Strategy

**Fallback conditions:**
- Only on NotFoundError from evaluator
- Other errors (timeout, execution failure) don't trigger fallback
- Preserves error information from failed evaluator

**Fallback rationale:**
- NotFoundError indicates function not available in current evaluator
- Other errors indicate actual execution problems
- Fallback only makes sense for missing functions
- Prevents masking real errors

### Multi-Executor FunctionConfig

A single FunctionConfig can define multiple executor types for different tags:

```yaml
apiVersion: config.porch.kpt.dev/v1alpha1
kind: FunctionConfig
metadata:
  name: apply-setters
  namespace: porch-fn-system
spec:
  image: apply-setters
  prefixes:
    - ""
    - ghcr.io/kptdev/krm-functions-catalog
  binaryExecutor:
    tags:
      - v0.2.0
    path: apply-setters
  podExecutor:
    tags:
      - v0.2.2
    timeToLive: 30m
```

In this example:
- `apply-setters:v0.2.0` executes via binary executor
- `apply-setters:v0.2.2` executes in a pod
- Other tags fall back to pod executor if available

## Wrapper Server

Provides gRPC interface for function execution inside pods.

### Wrapper Server Architecture

```
┌─────────────────────────────────────────┐
│         Function Pod                    │
│                                         │
│  ┌──────────────────┐                   │
│  │  Init Container  │                   │
│  │                  │                   │
│  │  • Copy wrapper  │                   │
│  │    server binary │                   │
│  │  • To shared vol │                   │
│  └────────┬─────────┘                   │
│           ↓                             │
│  ┌──────────────────┐                   │
│  │ Main Container   │                   │
│  │                  │                   │
│  │  Entrypoint:     │                   │
│  │  wrapper-server  │                   │
│  │                  │                   │
│  │  Args:           │                   │
│  │  • --port 9446   │                   │
│  │  • --            │                   │
│  │  • [function     │                   │
│  │     entrypoint]  │                   │
│  └──────────────────┘                   │
└─────────────────────────────────────────┘
```

**Wrapper server responsibilities:**
- Accept gRPC EvaluateFunction requests
- Execute function entrypoint with ResourceList
- Capture stdout/stderr from function
- Parse structured results from output
- Return gRPC response with results
- Provide health check endpoint

### Entrypoint Wrapping

The wrapper server wraps the original function entrypoint:

**Entrypoint characteristics:**
- Original function entrypoint preserved
- Passed as arguments after separator
- Executed as subprocess
- Stdin/stdout/stderr captured

### Resource List Processing

**Processing characteristics:**
- ResourceList passed as raw bytes to stdin
- Function reads from stdin
- Function writes to stdout
- Standard KRM function protocol
- No modification of ResourceList format

### Structured Results Handling

The wrapper server parses structured results from function output:

**Structured results:**
- ResourceList contains results array
- Each result has severity, message, tags
- Exit code indicates overall success/failure
- Results provide detailed feedback

**Exit code semantics:**
- **0**: Function succeeded
- **Non-zero**: Function failed
- **Parse failure**: Wrapper server error
- **Execution error**: System error

## Execution Characteristics

### Synchronous Execution

**All evaluators execute synchronously:**
- Block until function completes
- No async callbacks or futures
- Simple request-response pattern
- Caller waits for result

**Benefits:**
- Simple programming model
- Easy error handling
- Predictable behavior
- No concurrency complexity

### Context Cancellation

**Context propagation:**
- Context passed through all layers
- Execution stops on cancellation
- Resources cleaned up
- Error returned to caller

### Timeout Handling

**Timeout sources:**
- Task Handler sets context deadline
- Function Runner respects deadline
- Evaluators check context
- Function execution cancelled on timeout

**Timeout behavior:**
- Execution stops immediately
- Partial results discarded
- Timeout error returned
- Resources cleaned up

## Error Handling

The evaluation system handles errors at multiple levels.

### Error Types

**NotFoundError:**
- Function not available in evaluator
- Triggers fallback in multi-evaluator
- Distinguishable from execution errors
- Final NotFoundError if all evaluators fail

**Execution errors:**
- Function failed during execution
- Non-zero exit code
- Invalid output format
- Returned immediately without fallback

**Timeout errors:**
- Execution exceeded deadline
- Context cancellation triggered
- Partial results discarded
- Resources cleaned up

**System errors:**
- Infrastructure problems
- Pod creation failures
- Network issues
- Kubernetes API errors

### Error Propagation

**Error flow:**
- Function execution error captured
- Wrapper server formats error message
- Pod evaluator adds context
- Multi-evaluator checks error type
- Function runner returns to task handler
- Task handler includes in RenderStatus

### Error Recovery

**Retry mechanisms:**
- NotFoundError triggers fallback to next evaluator
- Transient errors may succeed on retry
- Waitlist allows retry on next request

**No retry scenarios:**
- Execution errors (function failed)
- Timeout errors (deadline exceeded)
- System errors (infrastructure problems)

## Performance Optimization

The evaluation system employs several performance strategies.

### Pod Reuse

**Reuse benefits:**
- Eliminates pod startup latency
- Avoids image pull time
- Reduces Kubernetes API load
- Improves response time

**Reuse mechanism:**
- Pod cache with TTL
- TTL extended on each use
- Garbage collection removes expired pods
- Failed pods immediately deleted

### Cache Warming

**Warming strategy:**
- Pre-create pods for frequently-used functions
- FunctionConfig CRDs specify functions and TTLs
- Concurrent pod creation at startup
- Reduces first-request latency

**Warming benefits:**
- Pods ready before first request
- Predictable performance
- No cold start penalty
- Better user experience

### Concurrent Requests

**Concurrency handling:**
- Multiple requests can execute concurrently
- Each request gets own gRPC connection
- Pod cache manager coordinates access
- Round-robin load balancing across equal-load pods

**Concurrency characteristics:**
- Same function, same pod: Parallel execution supported
- Same function, different pods: Concurrent
- Different functions: Fully concurrent
- No artificial concurrency limits

**Load balancing:**
- Requests distributed to least-loaded pods
- Round-robin among pods with equal load
- Ensures even work distribution
- Prevents hotspotting on single pod

### Resource Limits

**Resource considerations:**
- Function pods have resource limits
- Limits prevent resource exhaustion
- Configurable via pod template
- Affects concurrent execution capacity

**Performance tuning:**
- Adjust pod TTL via FunctionConfig for reuse frequency
- Configure cache warming via FunctionConfig for hot functions
- Use executable evaluator for critical path
- Monitor pod resource usage

## FunctionConfig CRD

The FunctionConfig CRD is the central configuration mechanism for function evaluation in both function-runner and porch-server.

### CRD Structure

**Core fields:**
- **image**: Base image name (without prefix or tag)
- **prefixes**: List of registry prefixes to match
- **binaryExecutor**: Configuration for binary execution
- **podExecutor**: Configuration for pod execution
- **goExecutor**: Configuration for native Go execution (porch-server only)

### Executor Types

**Binary Executor:**
- Maps specific image tags to local binary executables
- **tags**: List of image tags to handle
- **path**: Absolute or relative path to binary
- Used by function-runner for fast local execution

**Pod Executor:**
- Runs functions in containers with configurable settings
- **tags**: List of image tags to handle
- **timeToLive**: Duration pods remain cached (default: 30m)
- **templateOverrides**: Customize pod/container specifications
- Used by function-runner for containerized execution

**Go Executor:**
- Executes functions as native Go calls
- **tags**: List of image tags to handle
- **id**: Registration ID for Go function (defaults to image name)
- Used by porch-server for built-in functions

### Configuration Reconciliation

Both function-runner and porch-server embed a FunctionConfig reconciler:

**Reconciler behavior:**
- Watches FunctionConfig CRDs in the function namespace
- Populates internal cache on create/update/delete
- Maps full image references (with prefix and tag) to executor configurations
- Updates status with observedGeneration for each component

**Cache lookup during evaluation:**
1. Function requested with image reference
2. Image prefix resolved (if needed)
3. Cache queried with full image reference
4. Best matching FunctionConfig entry selected
5. Executor and configuration determined
6. Fallback to next evaluator on NotFoundError

**Example: Multi-tag configuration:**

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
  binaryExecutor:
    tags:
      - v0.4.2
    path: set-namespace
  podExecutor:
    tags:
      - v0.4.1
    timeToLive: 30m
  goExecutor:
    id: set-namespace
    tags:
      - v0.4
      - v0.4.5
```

This configuration supports:
- Binary execution for `set-namespace:v0.4.2`
- Pod execution for `set-namespace:v0.4.1`
- Native Go execution for `set-namespace:v0.4` and `v0.4.5` (porch-server only)
- Automatic prefix resolution for both qualified and unqualified images
