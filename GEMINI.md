# Argo Workflows

## Project Overview

Argo Workflows is an open source container-native workflow engine for orchestrating parallel jobs on Kubernetes. It is implemented as a Kubernetes CRD (Custom Resource Definition).

*   **Purpose:** Define workflows where each step is a container. Model multi-step workflows as a sequence of tasks or capture dependencies using a DAG.
*   **Key Technologies:**
    *   **Language:** Go (v1.24.10)
    *   **Frontend:** Vue.js (implied from UI build steps)
    *   **Infrastructure:** Kubernetes, Docker
    *   **Persistence:** MySQL, PostgreSQL, SQLite
    *   **Artifacts:** S3, MinIO, GCS, Azure Blob Storage, etc.

## Building and Running

The project uses a `Makefile` for most operations.

### Key Make Targets

*   **Build CLI:** `make cli` (Output: `dist/argo`)
*   **Build Controller:** `make controller` (Output: `dist/workflow-controller`)
*   **Build Executor:** `make argoexec` (Output: `dist/argoexec`)
*   **Run Locally:** `make start`
    *   Runs the workflow-controller and argo-server locally.
    *   Requires `kit` to be installed (`make kit`).
    *   Uses `RUN_MODE=local` by default.
*   **Install to K8s:** `make install`
    *   Installs Argo to the current Kubernetes cluster context.
*   **Clean:** `make clean` (Removes build artifacts)

### Running Tests

*   **Unit Tests:** `make test`
*   **CLI Tests:** `make test-cli`
*   **E2E Tests:** `make Test<Name>` (e.g., `make TestSuite`) or `make test-e2e` (check Makefile for exact target if needed)

## Development Conventions

*   **Code Style:**
    *   Go code is linted using `golangci-lint`. Run `make lint-go` to check.
    *   UI code is linted using `yarn lint`. Run `make lint-ui` to check.
    *   Markdown is checked with `markdownlint` and `markdown-link-check`.
*   **Code Generation:**
    *   Uses `go generate` and other tools (controller-gen, mockery, protobuf).
    *   Run `make codegen` to regenerate code, mocks, and manifests after changing APIs or interfaces.
*   **Dependencies:**
    *   Managed via `go.mod`.
    *   New dependencies must be actively maintained, have acceptable licenses (e.g., MIT), and be secure.
*   **Contribution:**
    *   See `docs/CONTRIBUTING.md` for detailed guidelines.
    *   Changes require unit or E2E tests.
    *   Sign-off on commits is often required (DCO).

## Key Directories

*   `cmd/`: Main entry points (`argo`, `workflow-controller`, `argoexec`).
*   `pkg/`: Core library code.
*   `workflow/`: Workflow logic and controller implementation.
*   `ui/`: Frontend application code.
*   `manifests/`: Kubernetes manifests (CRDs, install yamls).
*   `docs/`: Documentation.
*   `test/`: E2E and integration tests.
