"""Exercise persisted ERP business state on a disposable CI volume."""

from contextlib import contextmanager
from http.cookiejar import CookieJar
import json
import subprocess
import sys
import time
from urllib.error import URLError
from urllib.request import HTTPCookieProcessor, Request, build_opener
from uuid import uuid4


DATA_PATH = "/api/stations/erp/data?view=back"
PURCHASE_PATH = "/api/stations/erp/purchases"


def docker(*args):
    return subprocess.check_output(["docker", *args], text=True, timeout=60).strip()


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


def main(image):
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

            candidate_volume = docker("volume", "create", "erp-candidate-ci-" + uuid4().hex)
            try:
                snapshot = "candidate.sqlite"
                docker("exec", active, "/srv/station/station-server", "--backup", f"/srv/station/data/{snapshot}")
                docker(
                    "run", "--rm",
                    "--mount", f"type=volume,source={volume},target=/source,readonly",
                    "--mount", f"type=volume,source={candidate_volume},target=/srv/station/data",
                    image, "cp", f"/source/{snapshot}", "/srv/station/data/stations.sqlite",
                )
                with running_image(image, candidate_volume) as (candidate, _):
                    replay = call(browser, candidate, PURCHASE_PATH, order)
                    assert replay["id"] == created["id"]
                    approved = call(browser, candidate, PURCHASE_PATH, {
                        "action": "approve", "requestId": str(uuid4()), "poId": created["id"],
                    })
                    assert approved["status"] == "approved"
                current = call(browser, base, DATA_PATH)
                assert next(row for row in current["purchaseOrders"] if row["id"] == created["id"])["status"] == "draft"
            finally:
                docker("volume", "rm", candidate_volume)

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


if __name__ == "__main__":
    main(sys.argv[1])
