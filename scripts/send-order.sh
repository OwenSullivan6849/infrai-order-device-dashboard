#!/bin/sh
set -eu

base_url="${DASHBOARD_URL:-http://localhost:8080}"

curl --fail-with-body --request POST "${base_url}/orders/status" \
  --header 'Content-Type: application/json' \
  --data '{"order_id":"ord-42","device_id":"pos-7","account_id":"shop-9","stage":"checkout","status":"paid","sequence":1}'
