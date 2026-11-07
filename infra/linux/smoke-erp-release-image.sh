#!/usr/bin/env bash
set -euo pipefail

image=${1:?local ERP image required}
architecture=${2:?expected image architecture required}
revision=${3:?expected source revision required}

test "$(docker image inspect --format '{{.Architecture}}' "$image")" = "$architecture"
test "$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image")" = "$revision"

container="$(docker run --detach --publish 127.0.0.1::8095 "$image")"
trap 'docker rm --force "$container" >/dev/null' EXIT
address="$(docker port "$container" 8095/tcp)"
base="http://$address"
for attempt in $(seq 1 30); do
    if curl --fail --silent --max-time 3 "$base/api/health" >/dev/null; then
        break
    fi
    sleep 1
done

curl --fail --silent --show-error --max-time 5 "$base/api/health" | node -e '
const assert = require("node:assert/strict");
const health = JSON.parse(require("node:fs").readFileSync(0, "utf8"));
assert.equal(health.success, true);
assert.equal(health.data.backend, "go");
assert.equal(health.data.status, "ready");
assert.equal(health.data.revision, process.argv[1]);
' "$revision"

test "$(curl --silent --show-error --max-time 5 --output /dev/null --write-out '%{http_code}' "$base/erp/login")" = 200
test "$(curl --silent --show-error --max-time 5 --output /dev/null --write-out '%{http_code}' "$base/api/stations/erp/data")" = 401
test "$(curl --silent --show-error --max-time 5 --output /dev/null --write-out '%{http_code}' "$base/mes/login")" = 404
echo "Verified ERP $architecture image at $revision and its route boundaries."
