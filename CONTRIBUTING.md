# How to Contribute

We'd love to accept your patches and contributions to this project. There are just a few small guidelines you need to follow.

## Contributor License Agreement

Contributions to this project must be accompanied by a Contributor License Agreement (CLA). You (or your employer) retain the copyright to your contribution; this simply gives us permission to use and redistribute your contributions as part of the project. Head over to <https://cla.developers.google.com/> to see your current agreements on file or to sign a new one.

You generally only need to submit a CLA once, so if you've already submitted one (even if it was for a different project), you probably don't need to do it again.

All submissions to Google Open Source projects need to follow Google's Contributor License Agreement (CLA), which covers any original work of authorship included in the submission. This doesn't prohibit the use of coding assistance tools, including tool-, AI-, or machine-generated code, as long as these submissions abide by the CLA's requirements.

## Reporting Issues & Discussing Changes

- **Bugs & Feature Requests**: Please file an issue on GitHub Issues.
- **Large Contributions**: For significant changes or new features, please open an issue to discuss the design and approach with maintainers before submitting a pull request.

## Code Reviews & Pull Request Conventions

All submissions, including submissions by project members, require review. We use GitHub pull requests for this purpose. Consult [GitHub Help](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/proposing-changes-to-your-work-with-pull-requests/about-pull-requests) for more information on using pull requests.

- **Target Branch**: Open pull requests against the `main` branch. Bug fix backports to a `release-X.Y` branch should always land on `main` first before being cherry-picked.
- **Commit & PR Title Format**: Pull requests are squash-merged into `main`. Please format your PR title and commit messages following [Conventional Commits](https://www.conventionalcommits.org/) (`type(scope): short description`, such as `feat(translator): support custom resource mappings` or `fix(poller): handle empty partition list`).

## Community Guidelines

This project follows [Google's Open Source Community Guidelines](https://opensource.google/conduct/).

## Development Workflow

### Prerequisites

- **Go** (version 1.23+)
- **GNU Make**
- **Docker** (or compatible container engine like Podman)

*(Note: Developer tools such as `controller-gen`, `kustomize`, `golangci-lint`, and `setup-envtest` are downloaded and managed automatically by the `Makefile` into `./bin/`.)*

### Testing & Linting

Before submitting a pull request, ensure all tests, linters, and generated manifests pass:

```bash
# Run unit and controller tests (uses envtest)
make test

# Run code style and linter checks
make lint

# If modifying API types (api/v1alpha1/*_types.go), RBAC markers, or VERSION:
make manifests generate version-sync

# Run end-to-end tests (requires a dedicated, isolated Kind cluster)
make test-e2e
```
