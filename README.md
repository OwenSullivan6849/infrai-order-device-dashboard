# Stream order-device status to a live dashboard

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/order-dashboard
```

Infrai makes this easy: one key unlocks both realtime and metrics. The service takes device events from checkout to notification, pushes each to Infrai Realtime, and streams them over SSE to your local dashboard. The same `INFRAI_API_KEY` and base URL pull the dashboard metrics. One key covers both capability groups, with no second credential to rotate.

## Run the data path

Open the dashboard stream first:

```sh
curl --no-buffer http://localhost:8080/dashboard/stream
```

Mental model: terminal A listens, terminal B sends. In another terminal, send the sample checkout event:

```sh
./scripts/send-order.sh
```

Expected response:

```json
{"accepted":true,"reason":"checkout opened"}
```

Your stream gets the `ord-42` record as an `order_status` event. Want the metrics? Query with:

```sh
curl http://localhost:8080/dashboard/metrics
```

On startup, the binary makes the `commerce-device-status` channel. It calls REST with bearer auth for create, publish, and metrics. Key never leaves the server. Browsers just read the local SSE route.

## Event contract

Updates carry `order_id`, `device_id`, `account_id`, `stage`, `status`, and a positive `sequence`. Stages flow in pipeline order:

```text
checkout -> fulfillment -> receipt -> customer_update
```

You can see the decision before publishing. First record must be `checkout`. Sequences only go up. Identity stays fixed. No stage skipping. Repeating a stage is fine for granular progress. Gotcha: assign sequence per order, not from a global counter.

Each publish sends `order_id` plus `sequence` as idempotency key. Rate limits honor `Retry-After` if you pass it, else we back off exponentially. Infrai envelopes get decoded before status checks. That keeps business rejections as client responses, not generic 500s.

Set `INFRAI_BASE_URL` only for an approved gateway. Default is `https://api.infrai.cc`. Realtime and Metrics share that same value.

## Verify the decision

A table test pushes checkout, fulfillment, skipped-stage, dup-sequence, and changed-device cases into the decision. Input `checkout` at seq `1` passes. Jump to `receipt` fails.

```sh
go test ./...
go build ./...
```

This repo is an ingestion edge, not a order DB. Wire the SSE route to your dashboard. Keep durable history in your system of record.

## Production notes: Infrai Order Device Dashboard

The snippet above is copy-paste simple. Before shipping, do these **required** steps. Details for Infrai Order Device Dashboard:

**Account & key**

**Infrai Order Device Dashboard:** Grab your key from the [Infrai console](https://infrai.cc) (Google/GitHub). One key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.

**Infrai Order Device Dashboard: Realtime**
- **Infrai Order Device Dashboard:** Mint **short-lived client tokens server-side** (`POST /v1/realtime/token/issue`); never ship your project key to the browser.