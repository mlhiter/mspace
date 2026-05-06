# mspace

mspace is an Inbox and Issue workspace for software teams that want coding agents to develop locally and validate changes in real Kubernetes test environments instead of abstract sandboxes.

It takes the interaction shape of Multica, where humans and agents collaborate around shared issues and progress updates, and combines it with the deployment and validation shape of Optio, where projects can be exercised against controlled cluster resources. The product direction is narrower than both: each issue can attach an agent session, the current development path is local-first, and the deployed test target is a namespace-scoped Kubernetes environment in a shared cluster.

## Core Idea

AI coding work should live in a shared team workspace, not only in a repository checkout or a terminal transcript. The issue itself should be the durable document: context, discussion, agent progress, runtime evidence, PR output, and environment links all belong in one place.

## Why K8s

The Kubernetes environment is the deployment and test target that makes mspace worth building. Agents should be able to take locally developed changes, deploy them into a scoped namespace, and validate them with real cluster resources, real logs, real events, and real rollout state.

## First Product Promise

For each project, mspace gives a team a document-style issue workflow where a coding agent can:

- receive work through an inbox and issue flow;
- collaborate through comments, status updates, and blockers;
- run in a local development runtime by default;
- deploy and inspect only the assigned Kubernetes namespace with a scoped kubeconfig and ServiceAccount;
- deploy or update the project in a test cluster;
- inspect pods, services, ingress, events, and logs;
- keep open the future option of running the agent runtime inside Kubernetes;
- produce a PR, branch link, and environment evidence.

## Documentation

- [Product Brief](docs/product.md)
- [Architecture Notes](docs/architecture.md)
- [MVP Information Architecture](docs/ia.md)
- [Reference Notes](docs/references.md)
