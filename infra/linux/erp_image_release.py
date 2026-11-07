"""Resolve and verify an immutable ERP release on its rootless OCI runtime."""

import argparse
import json
import os
from pathlib import Path
import pwd
import re
import shutil
import stat
import subprocess
import sys


PACKAGE = "ghcr.io/davis1233798/business-suite-erp-station"
REVISION = re.compile(r"[0-9a-f]{40}")
DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
RUNTIME_ACCOUNT = "erp-runtime"
RUNTIME_HOME = Path("/var/lib/erp-runtime")
FREE_BYTES_FLOOR = 4 * 1024**3


def release_reference(revision, digest):
    if not REVISION.fullmatch(revision) or not DIGEST.fullmatch(digest):
        raise ValueError("Expected a full commit SHA and SHA-256 image manifest digest")
    return f"{PACKAGE}@{digest}"


def verified_image_id(revision, digest, image):
    reference = release_reference(revision, digest)
    if image.get("Os") != "linux" or image.get("Architecture") != "arm64":
        raise ValueError("ERP OCI requires a Linux ARM64 image")
    if reference not in (image.get("RepoDigests") or []):
        raise ValueError("Image metadata does not match the pinned ERP package digest")
    labels = (image.get("Config") or {}).get("Labels") or {}
    if labels.get("org.opencontainers.image.revision") != revision:
        raise ValueError("Image label does not match the requested ERP revision")
    image_id = image.get("Id") or ""
    if not DIGEST.fullmatch(image_id):
        raise ValueError("ERP image lacks an immutable local image ID")
    return image_id


def runtime_docker():
    uid = os.geteuid()
    account = pwd.getpwuid(uid)
    if uid == 0 or account.pw_name != RUNTIME_ACCOUNT or Path(account.pw_dir) != RUNTIME_HOME:
        raise ValueError("ERP image delivery must run as its dedicated runtime account")
    if RUNTIME_HOME.resolve(strict=True) != RUNTIME_HOME:
        raise ValueError("ERP runtime home cannot be a symlink")
    runtime_dir = Path("/run/user") / str(uid)
    socket = runtime_dir / "docker.sock"
    socket_status = socket.lstat()
    if not stat.S_ISSOCK(socket_status.st_mode) or socket_status.st_uid != uid or socket_status.st_mode & 0o077:
        raise ValueError("ERP rootless socket is not owned and restricted to its runtime account")

    def docker(*args, timeout=15):
        return subprocess.check_output(
            ["/usr/bin/docker", "--host", f"unix://{socket}", *args],
            env={"PATH": "/usr/bin:/bin", "HOME": str(RUNTIME_HOME), "XDG_RUNTIME_DIR": str(runtime_dir)},
            text=True,
            timeout=timeout,
        ).strip()

    info = json.loads(docker("info", "--format", "{{json .}}"))
    if "name=rootless" not in info.get("SecurityOptions", []):
        raise ValueError("ERP image delivery requires a rootless Docker daemon")
    storage = Path(info["DockerRootDir"]).resolve(strict=True)
    if not storage.is_relative_to(RUNTIME_HOME):
        raise ValueError("ERP Docker storage must belong to its runtime home")
    if shutil.disk_usage(storage).free < FREE_BYTES_FLOOR:
        raise ValueError("ERP image pull refused below the 4 GiB free-space floor")
    return docker


def pull_verified_image(docker, revision, digest):
    reference = release_reference(revision, digest)
    docker("pull", "--platform", "linux/arm64", reference, timeout=300)
    images = json.loads(docker("image", "inspect", reference))
    if len(images) != 1:
        raise ValueError("Expected one image for the pinned ERP digest")
    return verified_image_id(revision, digest, images[0])


def pull_and_verify(revision, digest):
    release_reference(revision, digest)
    return pull_verified_image(runtime_docker(), revision, digest)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("revision")
    parser.add_argument("digest")
    args = parser.parse_args()
    try:
        print(pull_and_verify(args.revision, args.digest))
    except (ValueError, OSError, KeyError, subprocess.SubprocessError) as error:
        print(f"ERP image verification failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
