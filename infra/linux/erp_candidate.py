"""Run a private ERP candidate against a disposable copy of active data."""

from contextlib import contextmanager
import argparse
import fcntl
import json
import re
import subprocess
import sys
import time
from urllib.error import HTTPError, URLError
from urllib.request import ProxyHandler, build_opener
from uuid import uuid4

from erp_image_release import RUNTIME_HOME, pull_verified_image, release_reference, runtime_docker


DATA = "/srv/station/data"
SERVER = "/srv/station/station-server"
CANDIDATE_LABEL = "suite.erp.candidate=true"
http = build_opener(ProxyHandler({}))


def read_response(base, path):
    try:
        with http.open(base + path, timeout=3) as response:
            return response.status, response.read(65536)
    except HTTPError as error:
        with error:
            return error.code, b""


def wait_ready(base):
    for _ in range(30):
        try:
            status, body = read_response(base, "/api/health")
            if status == 200:
                health = json.loads(body)
                if health.get("success") is True and health.get("data", {}).get("status") == "ready":
                    return health["data"]
        except (URLError, OSError, ValueError):
            pass
        time.sleep(1)
    raise RuntimeError("ERP candidate did not become ready")


def copy_active_snapshot(docker, active, candidate_volume, candidate_image):
    mounts = json.loads(docker("inspect", "--format", "{{json .Mounts}}", active))
    data = [mount for mount in mounts if mount.get("Destination") == DATA and mount.get("Type") == "volume"]
    if len(data) != 1 or not re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9_.-]*", data[0].get("Name", "")):
        raise RuntimeError("Active ERP data must use a dedicated Docker volume")
    snapshot = "rehearsal-" + uuid4().hex + ".sqlite"
    try:
        docker("exec", active, SERVER, "--backup", f"{DATA}/{snapshot}", timeout=90)
        docker(
            "run", "--rm", "--network", "none", "--label", CANDIDATE_LABEL,
            "--mount", f"type=volume,source={data[0]['Name']},target=/source,readonly",
            "--mount", f"type=volume,source={candidate_volume},target={DATA}",
            candidate_image, "cp", f"/source/{snapshot}", f"{DATA}/stations.sqlite",
            timeout=90,
        )
    finally:
        docker("exec", active, "rm", "-f", "--", f"{DATA}/{snapshot}")


@contextmanager
def candidate_container(docker, image, active=None):
    volume = docker("volume", "create", "--label", CANDIDATE_LABEL, "erp-candidate-" + uuid4().hex)
    container = None
    try:
        if active is not None:
            copy_active_snapshot(docker, active, volume, image)
        container = docker(
            "run", "--detach", "--label", CANDIDATE_LABEL,
            "--memory", "384m", "--cpus", "0.5", "--pids-limit", "128",
            "--publish", "127.0.0.1::8095",
            "--mount", f"type=volume,source={volume},target={DATA}",
            image,
        )
        address = docker("port", container, "8095/tcp")
        if not re.fullmatch(r"127\.0\.0\.1:[0-9]{1,5}", address):
            raise RuntimeError("ERP candidate port is not a single loopback endpoint")
        base = "http://" + address
        wait_ready(base)
        yield base
    finally:
        try:
            if container is not None:
                docker("rm", "--force", container)
        finally:
            docker("volume", "rm", volume)


def rehearsal(docker, image, revision, active=None):
    with candidate_container(docker, image, active) as base:
        health = wait_ready(base)
        if health.get("backend") != "go" or health.get("revision") != revision:
            raise RuntimeError("ERP candidate health revision does not match the release")
        for path, expected in (
            ("/erp/login", 200),
            ("/api/stations/erp/data", 401),
            ("/mes/login", 404),
        ):
            status, _ = read_response(base, path)
            if status != expected:
                raise RuntimeError(f"ERP candidate returned {status} for {path}, expected {expected}")


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
            active = docker("ps", "--quiet", "--filter", "name=^/business-suite-erp$")
            if active and docker("inspect", "--format", '{{index .Config.Labels "suite.erp.service"}}', active) != "active":
                raise RuntimeError("Existing container is not the managed ERP service")
            image = pull_verified_image(docker, args.revision, args.digest)
            rehearsal(docker, image, args.revision, active or None)
    except (ValueError, RuntimeError, OSError, KeyError, subprocess.SubprocessError) as error:
        print(f"ERP rehearsal failed: {error}", file=sys.stderr)
        return 1
    print(f"ERP candidate verified at {args.revision}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
