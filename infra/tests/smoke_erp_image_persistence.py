"""Exercise persisted ERP business state on a disposable CI volume."""

from contextlib import contextmanager
from http.cookiejar import CookieJar
import json
from pathlib import Path
import subprocess
import sys
import time
from urllib.error import URLError
from urllib.request import HTTPCookieProcessor, Request, build_opener
from uuid import uuid4

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "linux"))
from erp_candidate import candidate_container, rehearsal
from erp_activate import ACTIVE_NAME, PRIVATE_BASE, SERVICE_LABEL, activate


DATA_PATH = "/api/stations/erp/data?view=back"
PURCHASE_PATH = "/api/stations/erp/purchases"


def docker(*args, timeout=60):
    return subprocess.check_output(["docker", *args], text=True, timeout=timeout).strip()


def call(browser, base, path, data=None):
    payload = None if data is None else json.dumps(data).encode()
    request = Request(base + path, payload, {"Content-Type": "application/json"})
    with browser.open(request, timeout=8) as response:
        result = json.load(response)
    if result.get("success") is not True:
        raise AssertionError(f"ERP returned an unsuccessful response for {path}")
    return result["data"]


@contextmanager
def running_image(image, volume):
    container = docker(
        "run", "--detach", "--publish", "127.0.0.1::8095",
        "--mount", f"type=volume,source={volume},target=/srv/station/data",
        image,
    )
    try:
        base = "http://" + docker("port", container, "8095/tcp")
        for _ in range(25):
            try:
                health = call(build_opener(), base, "/api/health")
                if health["status"] == "ready":
                    break
            except (URLError, OSError):
                time.sleep(1)
        else:
            raise AssertionError("ERP image did not become ready")
        yield base, container
    finally:
        # Remove the test container while leaving its data volume available.
        docker("rm", "--force", container)


def login(browser, base):
    accounts = call(browser, base, "/api/auth/test-accounts")
    admin = next(account for account in accounts if account["role"] == "admin")
    call(browser, base, "/api/auth/login", {
        "site": "erp", "email": admin["email"], "password": admin["password"],
    })


def main(image, revision):
    volume = docker("volume", "create", "erp-image-ci-" + uuid4().hex)
    try:
        browser = build_opener(HTTPCookieProcessor(CookieJar()))
        with running_image(image, volume) as (base, active):
            login(browser, base)
            data = call(browser, base, DATA_PATH)
            order = {
                "action": "create",
                "requestId": str(uuid4()),
                "supplierId": next(row["id"] for row in data["suppliers"] if row["isActive"]),
                "lines": [{
                    "itemId": next(row["id"] for row in data["items"] if row["isActive"]),
                    "quantity": 2,
                    "unitPrice": 103,
                }],
            }
            created = call(browser, base, PURCHASE_PATH, order)

            try:
                rehearsal(docker, image, "0" * 40, active)
            except RuntimeError as error:
                assert "revision" in str(error)
            else:
                raise AssertionError("a candidate with the wrong revision passed")
            assert not docker("ps", "-aq", "--filter", "label=suite.erp.candidate=true")
            assert not docker("volume", "ls", "-q", "--filter", "label=suite.erp.candidate=true")

            with candidate_container(docker, image, active) as candidate:
                replay = call(browser, candidate, PURCHASE_PATH, order)
                assert replay["id"] == created["id"]
                approved = call(browser, candidate, PURCHASE_PATH, {
                    "action": "approve", "requestId": str(uuid4()), "poId": created["id"],
                })
                assert approved["status"] == "approved"
            current = call(browser, base, DATA_PATH)
            assert next(row for row in current["purchaseOrders"] if row["id"] == created["id"])["status"] == "draft"
            rehearsal(docker, image, revision, active)

        with running_image(image, volume) as (base, _):
            data = call(browser, base, DATA_PATH)
            assert sum(row["id"] == created["id"] for row in data["purchaseOrders"]) == 1
            replay = call(browser, base, PURCHASE_PATH, order)
            assert replay["id"] == created["id"], "container replacement lost the retry result"
            data = call(browser, base, DATA_PATH)
            assert sum(row["id"] == created["id"] for row in data["purchaseOrders"]) == 1

            unrelated = build_opener(HTTPCookieProcessor(CookieJar()))
            login(unrelated, base)
            other_data = call(unrelated, base, DATA_PATH)
            assert all(row["id"] != created["id"] for row in other_data["purchaseOrders"])
        print("ERP purchase and retry survived container replacement; candidate approval and other-tenant reads were isolated.")
    finally:
        docker("volume", "rm", volume)

    test_activation_and_rollback(image, revision)


def test_activation_and_rollback(image, revision):
    with build_opener(HTTPCookieProcessor(CookieJar())) as browser:
        try:
            first = activate(docker, image, revision)
            login(browser, PRIVATE_BASE)
            data = call(browser, PRIVATE_BASE, DATA_PATH)
            order = {
                "action": "create",
                "requestId": str(uuid4()),
                "supplierId": next(row["id"] for row in data["suppliers"] if row["isActive"]),
                "lines": [{
                    "itemId": next(row["id"] for row in data["items"] if row["isActive"]),
                    "quantity": 2, "unitPrice": 103,
                }],
            }
            created = call(browser, PRIVATE_BASE, PURCHASE_PATH, order)
            try:
                activate(docker, image, "0" * 40, first)
            except RuntimeError as error:
                assert "revision" in str(error)
            else:
                raise AssertionError("failed activation was accepted")
            assert docker("inspect", "--format", "{{.Id}}", ACTIVE_NAME) == first
            assert call(browser, PRIVATE_BASE, PURCHASE_PATH, order)["id"] == created["id"]

            second = activate(docker, image, revision, first)
            assert first != second
            assert docker("inspect", "--format", "{{.State.Running}}", first) == "false"
            assert call(browser, PRIVATE_BASE, PURCHASE_PATH, order)["id"] == created["id"]
        finally:
            # This CI runner is disposable. Scope cleanup to the ERP fixture.
            for container in docker("ps", "-aq", "--filter", f"label={SERVICE_LABEL}").splitlines():
                docker("rm", "--force", container)
            for volume in docker("volume", "ls", "-q", "--filter", "label=suite.erp.release=true").splitlines():
                docker("volume", "rm", volume)


if __name__ == "__main__":
    main(*sys.argv[1:])
