# Release Process Simplification

This model follows the same approach used by the [opendatahub-operator/rhods-operator](https://github.com/opendatahub-io/opendatahub-operator) (Red Hat OpenShift AI), which uses automated daily syncs to pin component manifests to specific commit SHAs from upstream repos.

This approach is also more closely aligned with how the downstream (RHCL/Konflux) build system works, where image digests are automatically bumped via PRs. There may be an opportunity to use the same tooling (Tekton/Konflux) for upstream automation rather than maintaining separate GitHub Actions workflows.

## Core Principle

The release of kuadrant-operator should snapshot the current tested state, not assemble versions manually. CI validates main of everything daily. The release just tags that known-good combination. There should be no requirement to release child operators before creating a kuadrant release. Child component versions are whatever has been synced and tested, not independently coordinated releases.

## Pinned References

All references to child components must be pinned, even on the main branch:

- **Chart content**: synced from a specific commit SHA, recorded in the parent's config
- **Container images**: referenced by digest (`@sha256:...`) in `RELATED_IMAGE_*` env vars, not by tag

This ensures any commit to kuadrant-operator is a fully reproducible build. No floating tags or branch references in the build chain.

Each child component's configuration specifies two things: a **branch** (tells automation where to look for updates) and a **commit SHA** (tells the build exactly what to use). The branch is for automation. The SHA is for the build.

## Automated Sync

When a child repo's tracked branch receives new commits and all CI checks pass (tests, linting, image build), automation updates the pinned references in the parent:

1. Child repo CI completes successfully on its tracked branch
2. `repository_dispatch` triggers the parent (kuadrant-operator)
3. Parent runs the sync tool, copying the updated chart as-is, and updates the pinned image digest in `RELATED_IMAGE_*` env vars
4. A PR is created (or force-updated if one already exists)
5. CI validates the new combination
6. If CI passes, the PR can be auto-merged

Key rules:

- **Dispatch after child CI completes**: ensures the parent only picks up fully validated commits and that the container image exists in the registry for digest resolution
- **Single open PR per target branch and child**: one sync PR per target branch and child component (e.g. `sync/main/authorino-operator`, `sync/release-v1.5/authorino-operator`), force-updated on each trigger. No stale PRs accumulating
- **Auto-merge on green CI**: if all tests pass, the sync PR merges automatically, keeping the parent continuously up to date
- **Failure stays open**: if CI fails, the PR remains open for investigation
- **Continuous integration of components**: child component changes are integrated into the parent continuously, catching breaking changes early. The PR gate ensures broken updates do not land on main and provides a natural place for any additional changes required alongside the component update (e.g. API changes, configuration adjustments)

## Release Flow

Because all references are pinned and CI validates every sync, the main branch is always in a releasable state:

1. Verify CI is green on main
2. Tag
3. Build and publish

No version picking. No "what version of authorino-operator should we include?" The answer is always "whatever is currently committed and tested". If a child component had a commit that hasn't been synced yet, it's not part of this release. It will be in the next one.

## Image References

`RELATED_IMAGE_*` env vars on the kuadrant-operator Deployment are the single source of truth for all images:

- **Upstream**: defaults point to digests resolved at sync time
- **Downstream**: build system overrides with pinned digests from the internal registry
- **OLM bundles**: `relatedImages` section generated from the same env vars using digests (`--use-image-digests`)

The startup deployer reads these env vars and overrides child chart image defaults when rendering. No version numbers needed in this chain. Just image references.

## Child Repo Releases

Child repos maintain their own independent release processes for standalone users. When kuadrant-operator releases, it can optionally tag each child repo with a Kuadrant-scoped tag (e.g. `kuadrant-v1.5.1`) for traceability. These tags are an output of the release, not an input.

## What Changes from Today

| Aspect | Current | Proposed |
|--------|---------|----------|
| Release trigger | Manual: fill in 6+ dependency versions | Tag current main |
| Version coordination | Required: all child versions specified upfront | Not needed: whatever was tested is what ships |
| Image references | Tags (`:v1.5.0`) | Digests (`@sha256:...`) |
| Chart versions | Specified at release time | Already committed from sync automation |
| Child operator releases | Must happen before kuadrant release | Independent, tagged retroactively |
| Reproducibility | Depends on tag stability | Guaranteed by commit SHA + image digest |

## What Needs Building

1. Sync automation workflows with `repository_dispatch` in each child repo, triggered after CI completes
2. Commit SHA and image digest pinning in kuadrant-operator config
3. Single-PR-per-child automation (force-update existing PR, optional auto-merge)
4. `USE_IMAGE_DIGESTS=true` as default for release bundle builds
5. Simplified `make prepare-release` that snapshots current state rather than accepting version inputs
6. Optional retroactive tagging script for child repos
