# Independent ERP delivery on OCI

ERP publishes verified ARM64 and AMD64 images. The selected OCI host is
ARM64. On 2026-11-07 its system disk had 6.5 GiB free, no ERP runtime or
deployment account, and no listener on the proposed ERP loopback port 18096.
Recheck those conditions during provisioning. MES already uses 18095.

## Program boundaries

- Run ERP under a dedicated rootless runtime and a distinct data volume.
  Initially bind only `127.0.0.1:18096`. Keep public routing with the current
  portfolio deployment until data continuity and authenticated ERP flows are
  verified on the independent service.
- Only the ERP main release workflow may request a deployment. Use a
  separate SSH credential restricted to a gateway with fixed ERP status and
  deployment commands. The gateway may pass a validated commit SHA and
  digest to a host-owned script. It cannot take an image URL, arbitrary
  command, network, bind mount, or Compose document.
- Pull the published ERP image by digest, verify ARM64 and its revision label,
  and compare the running health revision to the release SHA. The image uses
  the existing ERP package name. If the registry requires authentication,
  use a host-local read credential scoped to that package.
- Check free space before pulling or snapshotting. Bound resource use and
  serialize ERP releases. Keep rollback images and data snapshots separate
  from disposable build cache. Reuse the existing production host without
  compiling ERP there.
- Preserve the ERP data volume across releases. Test the candidate against a
  disposable snapshot before switching the private endpoint. If a release
  fails a health or route check, keep the previous private revision and its
  records available. Public cutover requires a separate write freeze,
  compatibility rehearsal, and data continuity check.

## Remote acceptance

1. A valid ARM64 digest with the matching revision starts on the expected
   loopback endpoint. Anonymous ERP data returns 401 and other system routes
   return 404. A wrong package, architecture, digest, or revision is refused.
2. Install a synthetic ERP workspace in a disposable OCI-like runtime.
   Replace the image and verify the existing workspace remains accessible.
   Simulate a candidate health failure and verify the previous revision and
   synthetic records still answer.
3. Demonstrate that the gateway cannot execute an arbitrary command or
   operate on the rootful daemon or MES resources. Check that insufficient
   disk space fails before image/data changes.
4. Run the automatic main delivery and check the actual private OCI ERP
   health revision. Treat public routing and production data import as
   subsequent acceptance gates.

## Implementation status

The image verifier accepts a commit SHA and manifest digest and pulls from
the fixed ERP package. It checks the dedicated runtime account, rootless
socket ownership, daemon storage, and a 4 GiB free-space floor before the
pull. The Linux ARM64 image must have the expected registry digest and
revision label. The ARM64 release job exercises persisted purchase and retry
records across container replacement on a disposable volume.

The verifier does not start or replace a service. Provisioning, the SSH
gateway, candidate rollback, and automatic OCI activation remain separate
acceptance steps.

## Snapshot boundary

The ERP server provides a local operator command `--backup DESTINATION`.
It takes a consistent SQLite snapshot of the existing ERP database in WAL
mode and publishes a new file in the requested directory. The command
opens the source read-only, requires its ERP schema marker, and refuses to
replace existing files. A candidate can write to a copy in a separate volume
without changing the active database. CI exercises a real purchase and
retry through the restored copy before using this command in the deployment
gateway.
