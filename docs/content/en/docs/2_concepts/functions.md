---
title: "KRM Functions"
type: docs
weight: 7
description: |
  Understanding KRM functions in Porch: how functions transform and validate package resources.
---

## What are Functions in Porch?

**Functions** in Porch are [KRM (Kubernetes Resource Model) functions](https://github.com/kubernetes-sigs/kustomize/blob/master/cmd/config/docs/api-conventions/functions-spec.md) -
programs (usually containerized) that transform or validate Kubernetes resource manifests within a package's files. Functions are
declared in a package's Kptfile and executed by Porch when rendering the package.

Functions enable:
- Automated resource generation and modification
- Policy enforcement and validation
- Configuration customization without manual editing
- Repeatable, auditable transformations

For details on how to declare and configure functions in the Kptfile pipeline, see the [kpt functions documentation](https://kpt.dev/book/04-using-functions/).

## Function Configuration

Porch uses **FunctionConfig** custom resources to configure how functions execute.
These configurations determine which executor type should handle the given function images and provide executor-specific settings.

Each FunctionConfig defines:
- **Image and prefixes**: The function image name and optional registry prefixes
- **Executor configurations**: One or more executor types (pod, binary, or go) with associated settings
- **Version-specific settings**: Different executors and configurations for different image tags

Example FunctionConfig:

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
  binaryExecutor:
    tags:
      - v0.4.2
    path: set-namespace
  goExecutor:
    id: set-namespace
    tags:
      - v0.4
      - v0.4.5
```

Most Porch components include an embedded reconciler that watches FunctionConfig resources and populates the component's own internal cache.
This cache determines which executor to use for each function invocation.

## Function Execution in Porch

Porch executes functions using multiple executor types, determined by FunctionConfig settings.

**Pod Executor**: Runs functions in isolated containerized pods (the traditional approach):
- Functions execute in Kubernetes pods managed by the `function-runner` microservice
- Configurable with pod templates, resource limits, TTL settings, and maximum parallel executions
- Suitable for functions requiring container isolation or external dependencies

**Binary Executor**: Substitutes specific function image tags with local binary executables:
- Executes pre-built function binaries directly on the host system
- Provides improved performance by avoiding container overhead
- Configured with the file path to the function binary

**Go Executor**: Executes certain functions as native Go function calls within the porch-server process:
- Functions run in-process for maximum efficiency
- Only available for functions integrated as Go libraries
- No container or process overhead

Regardless of executor type, Porch passes the package's resources to [kpt](https://kpt.dev), which passes the resources on as a [ResourceList](https://github.com/kubernetes-sigs/kustomize/blob/master/cmd/config/docs/api-conventions/functions-spec.md#resourcelist) to each function in the pipeline in turn.
kpt executes the functions sequentially in the order declared in the Kptfile pipeline and passes the function results back to Porch, which stores them in the PackageRevisionResources's `status.renderStatus` field.
Pipeline execution is triggered automatically following creation or clone of a package revision, update of a package revision, and when a package revision is proposed.
kpt passes the function results back to Porch and Porch stores them in the PackageRevisionResources's `status.renderStatus` field.

## When Functions Execute

### Automatic rendering

This occurs when a Draft package revision is created (init, clone, or edit tasks), when package resources are modified by an update through the PackageRevisionResources API resource, or when a package revision is proposed.

### Manual rendering via render task

Users can add an explicit `render` task to force re-execution of the pipeline. Note that the `render` task is not persisted in the package revision's task list.

### Lifecycle constraints

Functions execute only on **Draft** package revisions. Proposed package revisions must be rejected back to **Draft** status to be eligible for rendering again. Published package revisions are immutable—their rendered state is frozen. Function results are preserved in the `status.renderStatus` of the PackageRevisionResources API resource across lifecycle transitions.

## Function Results in Porch

Function execution results are stored in the status of the PackageRevisionResources API resource:

```yaml
apiVersion: porch.kpt.dev/v1alpha1
kind: PackageRevisionResources
...
status:
  renderStatus:
    error: ""
    result:
      exitCode: 0
      items:
        - image: ghcr.io/kptdev/krm-functions-catalog/set-namespace:v0.4.5
          exitCode: 0
        - image: ghcr.io/kptdev/krm-functions-catalog/kubeconform:v0.1.3
          exitCode: 1
          results:
            - message: "Invalid resource configuration"
              severity: error
```

The `renderStatus` field contains:
- Overall exit code (0 for success, non-zero for failure)
- Per-function results including exit codes and validation messages
- Error details if function execution failed

{{< alert title="Note" color="primary" >}}
By default, render failures (including validation failures) prevent Draft package revisions from being created and PackageRevisionResources from
being updated. However, when **updating resources on an existing Draft** (e.g. via `porchctl rpkg push`),
adding the `porch.kpt.dev/push-on-render-failure: "true"` annotation **to the PackageRevision** allows persisting resources even when rendering fails,
enabling iterative development on incomplete packages.
{{< /alert >}}


## Key Points

- Functions are standard KRM functions declared in the Kptfile pipeline (see [kpt functions docs](https://kpt.dev/book/04-using-functions/))
- Function execution behavior is configured using FunctionConfig custom resources that specify executor types (pod, binary, or go)
- Most Porch components include an embedded FunctionConfig reconciler that populates internal caches to determine which executor handles each function
- Functions can execute via pod containers, local binaries, or in-process Go calls depending on configuration
- Functions automatically execute during package rendering on Draft package revisions
- Function results are stored in `status.renderStatus` of the PackageRevisionResources view of a package revision
- Published packages are immutable - functions don't re-execute after publication
- By default, render failures (including validation failures) block Draft package creation and package revision resource updates
- When updating resources on an existing Draft (e.g., `porchctl rpkg push`), the `porch.kpt.dev/push-on-render-failure: "true"` annotation allows persisting resources despite render failures
