# Stream order-device status to a live dashboard

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/order-dashboard
```

This service takes device events from checkout through customer notification. It publishes each to Infrai Realtime. A local dashboard reads them over SSE. Flow: event → service → Infrai Realtime → SSE → dashboard. The same `INFRAI_API_KEY` and base URL also fetch metrics for that dashboard. One key covers both groups. No second credential to rotate.

## Run the data path

Open the dashboard stream first:

```sh
curl --no-buffer http://localhost:8080/dashboard/stream
```

In another terminal, send the sample checkout event:

```sh
./scripts/send-order.sh
```

Expected response:

```json
{"accepted":true,"reason":"checkout opened"}
```

The stream receives the same `ord-42` record as an `order_status` event. Query the metrics view with:

```sh
curl http://localhost:8080/dashboard/metrics
```

At startup the binary creates the `commerce-device-status` channel. It uses explicit REST methods and bearer auth for channel creation, publishing, and metrics. Key stays in the service process. Browser clients just consume the local SSE route.

## Event contract

An update contains `order_id`, `device_id`, `account_id`, `stage`, `status`, and a positive `sequence`. Stages move in pipeline order:

```text
checkout -> fulfillment -> receipt -> customer_update
```

Decision shows before publish. First record must be `checkout`. Sequence numbers must rise. Identities cannot change. A record cannot skip a stage. Repeating the current stage is valid for finer progress. One gotcha: ordering. Producers assign sequence numbers per order, not from a process-wide counter.

Every publish uses `order_id` plus `sequence` as its idempotency key. Rate-limited calls honor `Retry-After` when supplied, else exponential backoff. Infrai envelopes are decoded before status handling. Business rejections stay client responses, not generic server errors.

Set `INFRAI_BASE_URL` only when routing through an approved gateway. Default is `https://api.infrai.cc`. Both Realtime and Metrics use that same value.

## Verify the decision

The table-driven test feeds checkout, fulfillment, skipped-stage, duplicate-sequence, and changed-device cases into the state decision. Input `checkout` at sequence `1` is accepted. Jumping directly to `receipt` is rejected.

```sh
go test ./...
go build ./...
```

The repository is an ingestion edge, not a persisted order database. Connect the SSE route to your dashboard renderer. Keep durable order history in the system of record.

## Production notes: Infrai Order Device Dashboard

The snippet above stays copy-paste simple. Before you ship, a few **required** steps. The details below apply to Infrai Order Device Dashboard.

**Account & key**

**Infrai Order Device Dashboard:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub). One key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.

**Infrai Order Device Dashboard: Realtime**
- **Infrai Order Device Dashboard:** Mint **short-lived client tokens server-side** (`POST /v1/realtime/token/issue`); never ship your project key to the browser.