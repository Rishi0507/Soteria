# Security review

Scope: the four core domain services, the shared libraries and the infrastructure
that runs them. Written against the code as it stands, not as intended. Anything
listed as **open** is a known gap, not an oversight.

## 1. Personal data

**What we hold.** One category only: customer contact details, and only in the
order rescue path. A `rescue.order.proposed.v1` event carries `customer.email`,
`customer.phone` and `customer_id`, because the notification service needs an
address to deliver to.

**Where it must not go.** The audit dossier. A dossier is written to be handed to
an insurer or a regulator, and a customer's email address is not theirs to
receive. `ledger.Redact` strips `customer.email` and `customer.phone` before the
event is recorded, keeps `customer_id` so "which order" stays answerable, and
lists what it removed so the document declares its own omissions rather than
appearing complete. The record hash covers the *original* payload, so redaction
cannot be used to alter history undetected.

**Where it still goes.** The rescue API response includes the rescue record. The
`Record.customer` field is unexported and excluded from JSON, so the storefront
never receives contact details — but this is enforced by a struct tag, not by a
test. **Open: add a test asserting no PII appears in any API response.**

**Retention.** Not implemented. The ledger is in memory and dies with the
process; a production deployment needs a retention policy on dossiers (the
broker policy in `infra/rabbitmq/definitions.json` expires audit messages after
30 days, which is a message TTL, not a data retention policy). **Open.**

## 2. Authentication and authorization

**Customer actions.** A rescue confirmation requires an HMAC-SHA256 consent token
derived from the rescue id under `CONSENT_SECRET`. The token is derived, never
stored, so a database leak does not yield valid tokens. A forged or missing token
returns 403 and no swap occurs. Compared with `hmac.Equal`, so the check is not
timing-dependent.

**The secret.** `CONSENT_SECRET` falls back to a development value with a loud
warning rather than refusing to start. That is right for local work and wrong for
production: with the default secret, anyone can mint a consent token and swap
another customer's order line. **Open: refuse to start with the default secret
when a production flag is set.**

**Operator actions.** The containment review queue (`/v1/containment/actions/…`)
takes an `actor` string in the request body and applies no authentication at all.
Anyone who can reach the port can approve a hold, reject one, or change the
auto-hold threshold. This is the largest open issue in the system. The `actor`
value is recorded in the audit trail, so the dossier currently attributes an
action to a name that nobody verified. **Open: the ops console needs real
authentication before this is exposed beyond localhost.**

**Service-to-service.** None. Services trust the broker and each other. Acceptable
inside one network boundary; not acceptable if any service is exposed.

## 3. Network exposure

CORS is off by default and, when configured, echoes only listed origins with
`Vary: Origin` and never allows credentials. The APIs authenticate with tokens in
the request body rather than cookies, so there is nothing for a hostile origin to
ride on.

Every service binds all interfaces on its port. In the compose file the broker's
management UI (15672) and all four service ports are published to the host, which
is right for a laptop and wrong for a shared network. **Open: bind to localhost or
put the services behind one gateway before deploying anywhere shared.**

## 4. Secrets

All credentials come from the environment: `SHOPIFY_ACCESS_TOKEN`,
`CONSENT_SECRET`, `GROQ_API_KEY`, the broker URL. None are committed; `.env` is
git-ignored and only `.env.example` is tracked. The compose file passes them
through without defaults, so a missing secret produces a clear failure rather
than a silent fallback.

Tokens are never logged. Log lines name the shop domain and location ids, not the
token. No credential is committed: the broker account is created from
`RABBITMQ_USER` / `RABBITMQ_PASSWORD` in `infra/.env`, and the compose fallback is
an obvious placeholder rather than a working password. **Open: nothing stops a
deployment from running on that placeholder; a real deployment must set both.**

## 5. Data integrity

The audit ledger hash-chains every event: each record's hash covers its sequence,
identity, timestamp, producer, payload hash and its predecessor's hash. Altering,
reordering, inserting or deleting any record breaks every hash after it, and
`Verify` names the first record that fails. The chain head is submitted to an
RFC 3161 timestamp authority, which turns "internally consistent" into "existed
by this date". If the authority is unreachable the dossier still generates and
says on its face that it is unanchored.

**Open: the ledger is in memory.** Everything above is true within one process
lifetime and worthless after a restart. Durable append-only storage is the
prerequisite for any of this being evidence.

## 6. Denial of service and resource limits

The shared HTTP client rate-limits and retries with backoff. The broker refuses
publishes before the disk fills rather than losing events, and `consumer_timeout`
prevents a stuck consumer from growing a queue without bound.

Every consumer queue is a quorum queue with `x-delivery-limit`, so the **broker**
counts deliveries and dead-letters a message once the budget is spent. This was
originally enforced in the consumer by reading `x-death`, which the broker only
sets after a message has already been dead-lettered: the count stayed at zero and
a message no handler could process cycled forever. The integration test against a
real broker caught it; the in-process bus could not have.

**Open: no rate limiting or request size limits on the service APIs.** A public
deployment needs both.

## 7. Supply chain

Dependencies are few and pinned: `amqp091-go`, `jsonschema`, `go-pdf/fpdf`,
`santhosh-tekuri/jsonschema`, plus the SQLite driver in the ingestion services.
All are widely used and none are forks. Images build from `golang:1.26-alpine`
onto `gcr.io/distroless/static-debian12:nonroot`: no shell, no package manager,
non-root by default.

**Open: no dependency scanning or image scanning in CI.**

## Summary of open items, most serious first

1. **No authentication on the containment review API** — anyone reachable can
   approve a hold or move the threshold, and the audit trail will record whatever
   name they supply.
2. **`CONSENT_SECRET` falls back to a known default** — with it, one customer's
   order can be altered by anyone.
3. **The audit ledger is not durable** — the integrity guarantees do not survive a
   restart.
4. **No retention policy** for dossiers or recorded events.
5. **All service ports published** in the compose file, including the broker's
   management UI. Right for a laptop, wrong for a shared network.
6. **No rate limiting** on service APIs; no dependency or image scanning in CI.

None of these block a local demo. All of them block a deployment.
