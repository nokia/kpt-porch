---
title: "Function Runner Functionality"
type: docs
weight: 3
description: |
  Overview of function runner functionality and detailed documentation pages.
---

The Function Runner provides four core functional areas that work together to execute KRM functions in isolated environments:

## Functional Areas

### Function Evaluation

Executes KRM functions through pluggable evaluator strategies:
- **Evaluator Interface**: Common contract for all function execution strategies
- **Pod Evaluator**: Executes functions in Kubernetes pods with wrapper server integration
- **Executable Evaluator**: Runs pre-cached function binaries locally for fast execution
- **Multi-Evaluator**: Chains evaluators with fallback logic (exec → pod)
- **Request Channel Pattern**: Channel-based communication for pod cache coordination
- **Wrapper Server Integration**: gRPC wrapper injected into function pods for structured execution

For detailed architecture and process flows, see [Function Evaluation]({{% relref "/docs/5_architecture_and_components/function-runner/functionality/function-evaluation.md" %}}).

### Function Configuration Management

Provides declarative configuration of function executors through Kubernetes CRDs:
- **FunctionConfig Reconciler**: Embedded controller watches FunctionConfig resources and maintains internal cache
- **Executor Selection Cache**: Maps function images to executor types and configuration settings
- **Pod Executor Configuration**: Template overrides, TTL settings, and maximum parallel executions per function
- **Binary Executor Configuration**: Path mapping for substituting container images with local executables
- **Go Executor Configuration**: Function ID registration for native go function execution
- **Image Prefix Matching**: Supports multiple image prefixes and tags per function configuration
- **Template Customization**: Per-function pod and service template overrides including security context, resources, and environment variables

For detailed configuration examples, see [Function Runner Configuration]({{% relref "/docs/6_configuration_and_deployments/configurations/components/function-runner-config/_index.md" %}}).

For integration with executor selection, see [Function Runner Interactions]({{% relref "/docs/5_architecture_and_components/function-runner/interactions.md" %}}).

### Pod Lifecycle Management

Manages function execution pods with caching and garbage collection:
- **Pod Cache Manager**: Orchestrates pod lifecycle via channel-based communication
- **Pod Manager**: Handles pod and service CRUD operations
- **Pod Creation**: Template-based pod creation with init container for wrapper server injection
- **Service Management**: ClusterIP service frontends for service mesh compatibility
- **TTL-Based Caching**: Reuses pods with configurable expiration and extension on use
- **Garbage Collection**: Periodic cleanup of expired pods and failed pod handling
- **Pod Warming**: Pre-creates pods for frequently-used functions
- **Template Overrides**: Applies FunctionConfig-specified customizations to pod and service templates

For detailed architecture and process flows, see [Pod Lifecycle Management]({{% relref "/docs/5_architecture_and_components/function-runner/functionality/pod-lifecycle-management.md" %}}).

### Image and Registry Management

Caches image metadata and handles private registry authentication:
- **Metadata Caching**: In-memory cache of image digests and entrypoints
- **Image Inspection**: Fetches manifests and configs from container registries
- **Private Registry Support**: Authentication using Docker config format
- **TLS Configuration**: Custom CA certificates for secure registry connections
- **Secret Management**: Creates and attaches image pull secrets to function pods
- **Registry Operations**: Handles manifest retrieval, authentication retry, and error handling

For detailed architecture and process flows, see [Image and Registry Management]({{% relref "/docs/5_architecture_and_components/function-runner/functionality/image-registry-management.md" %}}).

## How They Work Together

```
┌─────────────────────────────────────────────────────────┐
│              Function Runner Service                    │
│               ┌──────────────────┐                      │
│               │     Function     │                      │
│               │   Configuration  │                      │
│               │    Management    │                      │
│               │                  │                      │
│               │ • FunctionConfig │                      │
│               │   Reconciler     │                      │
│               │ • Executor Cache │                      │
│               │ • Image Prefix   │                      │
│               │   Matching       │                      │
│               └──────────────────┘                      │
│                        │                                │
│           ┌────────────┴─────────────┐                  │
│           ↓                          ↓                  │
│  ┌──────────────────┐      ┌──────────────────┐         │
│  │    Function      │      │      Pod         │         │
│  │   Evaluation     │ ───> │    Lifecycle     │         │
│  │                  │      │   Management     │         │
│  │  • Evaluator     │      │                  │         │
│  │    Selection     │      │  • Pod Cache     │         │
│  │  • Exec/Pod      │      │  • Pod Manager   │         │
│  │    Fallback      │      │  • GC/TTL        │         │
│  │  • Wrapper       │      │  • Service Mgmt  │         │
│  │    Server        │      │                  │         │
│  └──────────────────┘      └──────────────────┘         │
│           │                         │                   │
│           └────────┬────────────────┘                   │
│                    ↓                                    │
│          ┌──────────────────┐                           │
│          │     Image &      │                           │
│          │    Registry      │                           │
│          │   Management     │                           │
│          │                  │                           │
│          │  • Metadata      │                           │
│          │    Cache         │                           │
│          │  • Registry      │                           │
│          │    Auth          │                           │
│          │  • TLS Config    │                           │
│          │  • Pull Secrets  │                           │
│          └──────────────────┘                           │
└─────────────────────────────────────────────────────────┘
```

**Integration flow:**
1. **Function Evaluation** receives gRPC request from Task Handler
2. **Function Configuration Management** queries cache for function-specific configuration
3. **Multi-Evaluator** selects appropriate evaluator based on FunctionConfig settings
4. **If binary executor configured**, executes local function binary (fast path)
5. **If go executor configured**, invokes registered native go function
6. **If pod executor configured or no match**, falls back to pod evaluator (container execution)
7. **Pod Lifecycle Management** checks pod cache for existing pod matching configuration
8. **If cache miss**, creates new pod with FunctionConfig template overrides via Pod Manager
9. **Image & Registry Management** resolves image metadata and authentication
10. **Pod Manager** creates pod with image pull secrets and service frontend
11. **Pod Cache Manager** stores pod with FunctionConfig-specified TTL for reuse
12. **Function Evaluation** connects to pod via service and executes function
13. **Wrapper Server** executes function binary and returns structured results
14. **Garbage Collection** periodically removes expired pods based on TTL settings
 
Each functional area is documented in detail on its own page with architecture diagrams, process flows, and implementation specifics.
