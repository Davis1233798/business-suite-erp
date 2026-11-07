# ERP release images for ARM64 OCI

The selected OCI host runs ARM64. Its shared application deployment builds
images on the host, which uses space for build layers. The ERP release pipeline
will build and smoke-test an OCI-ready image on a native GitHub ARM64 runner.

## Program boundaries

- Preserve the existing main commit tag for the AMD64 image. Publish the ARM64
  image under the same commit with an `-arm64` suffix. Only the main push
  matching the current main revision may publish, after ERP and Windows checks.
- Bake the source revision into the station binary and image metadata. The
  health endpoint reports it only if it is a valid Git commit ID, and keeps
  reporting database readiness.
- Smoke-test each native image before publishing. Check its architecture, the
  health revision, an ERP route, an anonymous ERP data denial and rejection of
  a route from another system.
- Image publication is a prerequisite for OCI deployment. The next iteration
  must verify the registry digest and platform on the host, deploy to the ERP
  data boundary and prove persistence, rollback and routing before cutover.

GitHub Actions runs validation and builds. See
[GitHub-hosted ARM64 runners](https://docs.github.com/en/actions/reference/runners/github-hosted-runners#standard-github-hosted-runners-for-public-repositories).
