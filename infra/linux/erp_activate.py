"""Activate a verified ERP image on the private rootless OCI endpoint."""

import argparse
import fcntl
import re
import shutil
import subprocess
import sys
from uuid import uuid4

from erp_candidate import DATA, SERVER, active_data_volume, rehearsal, verify_endpoint
from erp_image_release import (
    FREE_BYTES_FLOOR, RUNTIME_HOME, pull_verified_image, release_reference, runtime_docker,
)


ACTIVE_NAME = "business-suite-erp"
PRIVATE_ADDRESS = "127.0.0.1:18096"
PRIVATE_BASE = "http://" + PRIVATE_ADDRESS
SERVICE_LABEL = "suite.erp.service=active"
RELEASE_VOLUME_LABEL = "suite.erp.release=true"


def managed_active(docker):
    container = docker("ps", "--all", "--quiet", "--no-trunc", "--filter", f"name=^/{ACTIVE_NAME}$")
    if not container:
        return None
    details = docker("inspect", "--format", '{{index .Config.Labels "suite.erp.service"}} {{.State.Running}}', container)
    if details != "active true":
        raise RuntimeError("The private ERP endpoint has a stopped or unmanaged container")
    return container


def require_snapshot_capacity(docker, active):
    storage = docker("info", "--format", "{{.DockerRootDir}}")
    required = FREE_BYTES_FLOOR
    if active:
        # Rehearsal holds both the online snapshot and the temporary copy.
        size = int(docker("exec", active, "du", "-sb", DATA).split()[0])
        required += 2 * size
    if shutil.disk_usage(storage).free < required:
        raise RuntimeError("Insufficient ERP runtime space for the release and snapshot")


def activate(docker, image, revision, active=None):
    previous_image = previous_revision = previous_volume = None
    if active:
        if managed_active(docker) != active:
            raise RuntimeError("Active ERP service changed before activation")
        previous_image = docker("inspect", "--format", "{{.Image}}", active)
        previous_revision = docker(
            "inspect", "--format", '{{index .Config.Labels "org.opencontainers.image.revision"}}', active,
        )
        if not re.fullmatch(r"[0-9a-f]{40}", previous_revision):
            raise RuntimeError("The managed ERP service lacks a valid rollback revision")
        previous_volume = active_data_volume(docker, active)
    elif managed_active(docker):
        raise RuntimeError("ERP already has an active release")

    release_id = uuid4().hex
    release_label = "suite.erp.release-id=" + release_id
    volume = docker("volume", "create", "--label", RELEASE_VOLUME_LABEL, "erp-release-" + release_id)
    try:
        if active:
            docker("stop", "--time", "10", active, timeout=25)
            # Copy only after the old writer has stopped. SQLite can create
            # WAL shared-memory metadata on the source mount; SQL is read-only.
            docker(
                "run", "--rm", "--network", "none",
                "--mount", f"type=volume,source={previous_volume},target=/source",
                "--mount", f"type=volume,source={volume},target={DATA}",
                "--env", "SUITE_DB=/source/stations.sqlite", "--entrypoint", SERVER,
                previous_image, "--backup", f"{DATA}/stations.sqlite", timeout=90,
            )
            docker("rename", active, "business-suite-erp-rollback-" + release_id)
        next_container = docker(
            "create", "--name", ACTIVE_NAME, "--label", SERVICE_LABEL, "--label", release_label,
            "--label", "org.opencontainers.image.revision=" + revision,
            "--memory", "384m", "--cpus", "0.5", "--pids-limit", "128",
            "--publish", PRIVATE_ADDRESS + ":8095",
            "--mount", f"type=volume,source={volume},target={DATA}", image,
        )
        docker("start", next_container)
        verify_endpoint(PRIVATE_BASE, revision)
        docker("update", "--restart", "unless-stopped", next_container)
        return next_container
    except Exception as activation_error:
        try:
            failed = docker("ps", "-aq", "--filter", f"label={release_label}")
            if failed:
                docker("rm", "--force", failed)
            if active:
                if docker("inspect", "--format", "{{.Name}}", active) != "/" + ACTIVE_NAME:
                    docker("rename", active, ACTIVE_NAME)
                if docker("inspect", "--format", "{{.State.Running}}", active) != "true":
                    docker("start", active)
                verify_endpoint(PRIVATE_BASE, previous_revision)
            docker("volume", "rm", volume)
        except Exception as rollback_error:
            raise RuntimeError(f"ERP activation failed and automatic rollback did not complete: {rollback_error}") from activation_error
        raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("revision")
    parser.add_argument("digest")
    args = parser.parse_args()
    try:
        release_reference(args.revision, args.digest)
        docker = runtime_docker()
        with (RUNTIME_HOME / "erp-release.lock").open("a") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            active = managed_active(docker)
            require_snapshot_capacity(docker, active)
            image = pull_verified_image(docker, args.revision, args.digest)
            require_snapshot_capacity(docker, active)
            if active and docker("inspect", "--format", "{{.Image}}", active) == image:
                verify_endpoint(PRIVATE_BASE, args.revision)
            else:
                rehearsal(docker, image, args.revision, active)
                require_snapshot_capacity(docker, active)
                activate(docker, image, args.revision, active)
    except (ValueError, RuntimeError, OSError, KeyError, subprocess.SubprocessError) as error:
        print(f"ERP activation failed: {error}", file=sys.stderr)
        return 1
    print(f"ERP private endpoint verified at {args.revision}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
