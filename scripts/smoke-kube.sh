#!/usr/bin/env bash
set -euo pipefail
# Requires manager and kubectl to use the same context and namespace.
base=${MANAGER_URL:-http://127.0.0.1:8080}
namespace=${NAMESPACE:-agent-env}
record=$(curl -fsS -H 'Content-Type: application/json' -d '{"template":"linux"}' "$base/v1/sandboxes")
id=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])' <<<"$record")
url="$base/v1/sandboxes/$id"
trap 'curl -fsS -X DELETE "$url" >/dev/null' EXIT
for ((attempt=0;attempt<90;attempt++)); do
 status=$(curl -fsS "$url" | python3 -c 'import json,sys; print(json.load(sys.stdin)["status"])')
 case "$status" in ready) break;; pending) sleep 1;; *) echo "unexpected state: $status" >&2; exit 1;; esac
done
[[ "$status" == ready ]]
[[ "$(kubectl -n "$namespace" exec "ae-$id" -- echo agent-env-ok)" == agent-env-ok ]]
curl -fsS -X DELETE "$url"
kubectl -n "$namespace" wait --for=delete "pod/ae-$id" --timeout=90s
[[ "$(curl -sS -o /dev/null -w '%{http_code}' "$url")" == 404 ]]
trap - EXIT
echo "PASS: create -> ready -> exec -> delete -> 404"
