---
title: "Interactions"
type: docs
weight: 4
description: |
  How the Porch API server interacts with other components.
---

## Overview

The Porch API Server acts as the integration point between Kubernetes clients and Porch's internal components. It translates Kubernetes API requests into Engine operations and manages watch streams for real-time event delivery.

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────┐
│              Porch API Server                           │
│                                                         │
│  ┌──────────────┐      ┌──────────────┐      ┌──────┐   │
│  │   Clients    │ ───> │     REST     │ ───> │Engine│   │
│  │   (kubectl,  │      │   Storage    │      │      │   │
│  │    kpt, etc) │      │              │      │      │   │
│  └──────────────┘      └──────────────┘      └──────┘   │
│         ↑                      │                │       │
│         │                      ↓                ↓       │
│         │              ┌──────────────┐      ┌──────┐   │
│         └──────────────│   Watcher    │      │Cache │   │
│                        │   Manager    │      │      │   │
│                        └──────────────┘      └──────┘   │
└─────────────────────────────────────────────────────────┘
```

## Engine Integration

The API Server delegates all package operations to the CaD Engine:

### Request Translation Pattern

```
Kubernetes API Request
        ↓
  REST Storage Handler
        ↓
  Strategy Validation
        ↓
  Engine Method Call
        ↓
  • CreatePackageRevision
  • UpdatePackageRevision
  • DeletePackageRevision
  • ListPackageRevisions
        ↓
  Engine Response
        ↓
  Convert to API Object
        ↓
  Return to Client
```

**Translation characteristics:**
The REST storage component translates API operations into calls for the Engine, with validation strategies ensuring data integrity before Engine invocation. The Engine then processes these calls and returns repository objects, which the REST storage converts into standard Kubernetes API objects before propagating any errors back to the client.

### Operation Mapping

**Create operations:**
- CreatePackageRevision → Engine package revision creation

**Read operations:**
- GetPackageRevision → Engine package revision listing (filtered by name)
- ListPackageRevisions → Engine package revision listing
- GetPackage → Engine package listing (filtered by name)
- ListPackages → Engine package listing

**Update operations:**
- UpdatePackageRevision → Engine package revision update
- UpdatePackageRevisionResources → Engine package resources update

**Delete operations:**
- DeletePackageRevision → Engine package revision deletion

**Watch operations:**
- WatchPackageRevisions → Engine cache watch for package revisions

### Context Propagation

Client requests carry a Kubernetes request context, from which the REST storage extracts user information before passing the entire context to the Engine for all subsequent operations. The Engine then leverages this context for critical functions such as managing cancellations and timeouts, incorporating user details into audit trails (PublishedBy), and facilitating comprehensive tracing and logging.

## Cache Integration

The API Server interacts with the Cache through the Engine:

### Repository Access Pattern

```
API Request
        ↓
  REST Storage
        ↓
  Engine
        ↓
  Cache.OpenRepository
        ↓
  Repository Operations
        ↓
  Cache Response
        ↓
  Engine Response
        ↓
  API Response
```

**Access characteristics:** The API Server interacts with the cache exclusively through the Engine, which is responsible for all cache operations, manages the repository lifecycle, and provides the API Server with abstract representations of the stored data.

### Background Synchronization

Repository synchronization is handled by the [Repository Controller]({{% relref "/docs/5_architecture_and_components/controllers/repository-controller/_index.md" %}}), a separate component that manages the Repository resource lifecycle.

**Integration pattern:**
- Repository Controller watches Repository CRs and triggers sync operations
- Sync operations update the Cache directly
- Cache propagates change notifications to the API Server
- API Server delivers watch events to connected clients

The API Server observes cache changes initiated by the Repository Controller rather than managing synchronization directly. See [Repository Controller]({{% relref "/docs/5_architecture_and_components/controllers/repository-controller/_index.md" %}}) for details on sync scheduling and configuration.

## Kubernetes API Integration

The API Server integrates with the Kubernetes API aggregation layer:

### API Aggregation Pattern

```
Kubernetes API Server
        ↓
  API Aggregation Layer
        ↓
  Porch API Server
        ↓
  • porch.kpt.dev/v1alpha1
  • config.porch.kpt.dev/v1alpha1
        ↓
  REST Storage Handlers
```

**Aggregation characteristics:** The Porch API Server is registered as an aggregated API, allowing the Kubernetes API Server to proxy requests to it. Authentication and authorization are handled by Kubernetes, ensuring that the Porch API Server only receives pre-authenticated requests.

### RBAC Integration

**Authorization flow:** The Kubernetes API Server enforces RBAC policies, ensuring that the Porch API Server only receives authorized requests. With user information available in the request context, Porch then applies additional business-specific authorization rules.

**RBAC resources:**
- **PackageRevision:** get, list, watch, create, update, delete
- **PackageRevisionResources:** get, list, watch, update
- **Package:** get, list, watch, create, delete

### Client Integration

**Client types:**
- **kubectl**: Standard Kubernetes CLI
- **kpt**: Package management CLI
- **Porchctl**: Porch-specific CLI
- **Custom controllers**: Automation and workflows

**Client operations:** Clients can perform CRUD (Create, Read, Update, Delete) operations on Porch resources, subscribe to watch streams for real-time updates, manage approval workflows for lifecycle transitions, and update package content.

## Watch Stream Management

See [Functionality]({{% relref "/docs/5_architecture_and_components/porch-apiserver/functionality.md#watch-stream-management" %}}) for detailed watch lifecycle and event delivery.

The API Server integrates watch streams between clients and Engine:

### Watch Integration Pattern

```
Client Watch Request
        ↓
  REST Storage.Watch()
        ↓
  Engine.ObjectCache().WatchPackageRevisions()
        ↓
  WatcherManager.Watch()
        ↓
  Register Watcher
        ↓
  Return Watch Interface
        ↓
  Client Receives Events:
        ↓
  • Added
  • Modified
  • Deleted
```

**Watch integration:** Clients can subscribe to watch streams using the standard Kubernetes watch API. The REST storage layer delegates these requests to the Engine's WatcherManager, which then filters and delivers events through a component chain. Resources are automatically cleaned up when a client disconnects.

### Event Delivery

**Event sources:**
- Package revision creation (Added events)
- Package revision updates (Modified events)
- Package revision deletion (Deleted events)
- Repository sync changes triggered by Repository Controller (all event types)

## Background Job Coordination

See [Functionality]({{% relref "/docs/5_architecture_and_components/porch-apiserver/functionality.md#background-operations" %}}) for detailed background operation implementation.

The API Server coordinates resource cleanup and other background operations. Repository synchronization is handled externally by the [Repository Controller]({{% relref "/docs/5_architecture_and_components/controllers/repository-controller/_index.md" %}}), which runs as a separate component using the controller-runtime framework.

### Repository Sync Flow

```
Repository Controller
        ↓
  Watch Repository CRs
        ↓
  Reconcile Loop
        ↓
  Trigger Sync via Cache
        ↓
  Cache Updates
        ↓
  Cache Sends Notifications
        ↓
  API Server WatcherManager
        ↓
  Clients Receive Events
```

**Integration flow:** The integration flow operates with the Repository Controller independently managing the synchronization lifecycle. This controller monitors Repository resources and performs reconciliations according to defined sync schedules. All synchronization operations directly update the Cache, and subsequent notifications from the Cache are propagated through the API Server to connected clients. It is important to note that the API Server's role is to observe and deliver these events, not to initiate the synchronization process itself.

### Cleanup Coordination

See [Functionality]({{% relref "/docs/5_architecture_and_components/porch-apiserver/functionality.md#resource-cleanup" %}}) for detailed cleanup operations.

**Integration flow:** When a repository deletion is detected through the Kubernetes API, the cleanup process is coordinated via the Engine and Cache components. Following this cleanup, notifications regarding the deletion are then propagated to all active watchers.

## Error Handling

See [Functionality]({{% relref "/docs/5_architecture_and_components/porch-apiserver/functionality.md#error-handling" %}}) for detailed error handling within the API Server.

The API Server translates errors across component boundaries:

### Engine Error Translation

**Error types:**
- **Validation errors**: Translated to 400 Bad Request
- **Not found errors**: Translated to 404 Not Found
- **Conflict errors**: Translated to 409 Conflict
- **Internal errors**: Translated to 500 Internal Server Error

**Translation pattern:** The goal of these error translations is to make sure the Engine returns typed errors instead of just generic 500 error codes. These errors are then converted by the REST storage layer into a standard Kubernetes Status object. This Status object is designed to include a clear error message along with any relevant details. Ultimately, the client interacting with the system receives this standardized Kubernetes error response, ensuring consistent error handling.

### Watch Error Handling

**Integration error handling:** Registration errors originating from the WatcherManager are returned directly to the client, while delivery errors lead to the closure and cleanup of the affected stream, and an automatic cleanup process is also initiated upon client disconnection.

### Background Job Errors

**Error types:**
- Cache operation failures
- Kubernetes API errors
- Resource cleanup failures

**Handling strategy:** The handling strategy for background job errors is to log them with their context. Operations continue for other resources despite an error, and any repository synchronization errors are specifically managed by the Repository Controller.

## Concurrency and Safety

See [Functionality]({{% relref "/docs/5_architecture_and_components/porch-apiserver/functionality.md#concurrency-control" %}}) for detailed concurrency mechanisms.

The API Server coordinates concurrent operations across components:

### Request Concurrency

Request concurrency is integrated in several ways, like managing request concurrency through the Engine, enforcing optimistic locking at the API Server and Engine boundary, isolating watch streams per client, and coordinating repository synchronization operations via the Repository Controller.

