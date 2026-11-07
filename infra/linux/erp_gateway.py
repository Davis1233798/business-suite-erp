"""Fixed forced-command entry point for the ERP deployment key."""

import json
import os
from pathlib import Path
import subprocess
import sys

from erp_gateway_protocol import MAX_REQUEST, parse_request
from erp_image_release import RUNTIME_ACCOUNT, RUNTIME_HOME, runtime_docker


INSTALL_DIR = Path("/usr/local/lib/business-suite-erp")
PYTHON = "/usr/bin/python3"
RUNTIME_COMMAND = [
    "/usr/bin/sudo", "-n", "-u", RUNTIME_ACCOUNT, PYTHON, "-Es",
    str(INSTALL_DIR / "erp_gateway.py"), "--runtime",
]


def ssh_request():
    request = os.environ.get("SSH_ORIGINAL_COMMAND", "")
    parse_request(request)
    result = subprocess.run(RUNTIME_COMMAND, input=request.encode("ascii"), check=False)
    return result.returncode


def runtime_request():
    request = sys.stdin.buffer.read(MAX_REQUEST + 1).decode("ascii")
    command, *arguments = parse_request(request)
    if command == "status":
        runtime_docker()
        print(json.dumps({
            "protocol": 1, "ready": True, "runtime_user": RUNTIME_ACCOUNT,
            "docker_mode": "rootless", "endpoint": "127.0.0.1:18096",
        }))
        return 0
    os.execve(PYTHON, [PYTHON, "-Es", str(INSTALL_DIR / "erp_activate.py"), *arguments], {
        "PATH": "/usr/bin:/bin", "HOME": str(RUNTIME_HOME),
    })


def main():
    try:
        if sys.argv[1:] == ["--ssh"]:
            return ssh_request()
        if sys.argv[1:] == ["--runtime"]:
            return runtime_request()
        raise ValueError("Invalid ERP gateway invocation")
    except (ValueError, OSError, KeyError, subprocess.SubprocessError) as error:
        print(f"ERP deployment request refused: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
