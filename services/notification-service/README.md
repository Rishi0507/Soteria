# notification-service

Turns containment, order-rescue and anti-evasion events into messages people act on, and records every delivery so the audit dossier can prove who was told, when, through which provider.

| Consumes (queue `notification.outbound`) | Who gets told | Channel |
|---|---|---|
| `containment.action.taken.v1` | retailer ops: hazard, decision, units quarantined vs. still sellable, failures | Slack |
| `rescue.order.proposed.v1` | the **customer**: recalled item in their order, the substitute/refund/cancel options, a link to choose; ops gets a masked copy | email (Resend), SMS fallback (printed — no provider yet), Slack |
| `evasion.flagged.v1` | retailer ops: recalled lot resurfacing on a marketplace, with evidence | Slack |

Produces `notification.delivered.v1` on `notification.x` for every terminal outcome (`SENT` or `FAILED`), correlated to the incident and caused by the source event — the audit-proof service can cite it as "customer notified at T, provider ref X".

## Delivery guarantees

- **Idempotent per (event, channel, recipient).** A redelivered event never re-sends; the SQLite ledger (`DB_PATH`) is checked first.
- **Transient failures** (network, 429, 5xx) retry 3× in-process with backoff, then the message is handed back to the bus (nack → redelivery → DLQ after the topology's retry budget). The ledger row stays `RETRYING` with the last error.
- **Permanent failures** (4xx, invalid recipient, no provider configured) are recorded as `FAILED`, emitted as a `FAILED` delivered event, and acked — no retry storms.
- **No contact details** on a rescue → ops is still alerted, with "customer notified via: none".
- Recipients are masked in logs and in the ops copy (`cu***@example.com`).

## Run

```sh
make replay          # print all messages for the sample incident (no keys needed)
make run             # long-running consumer; needs RABBITMQ_URL
curl localhost:8084/healthz
curl localhost:8084/v1/incidents/inc-2026-0912-001/deliveries   # ledger for the ops console / audit
```

Configuration (see `.env.example`): `SLACK_WEBHOOK_URL`, `RESEND_API_KEY`, `EMAIL_FROM`, `RESCUE_CONSENT_URL` (Person 2's consent page, `{rescue_id}` substituted), `RETAILER_NAME`, `RABBITMQ_URL`, `DB_PATH`, `PORT`. Without Slack/Resend keys the messages are printed to stdout and the log says so.

Providers: Slack incoming webhook (free), [Resend](https://resend.com) for email (free 3k/month; `onboarding@resend.dev` works as sender before a domain is verified). SendGrid was not used because its free tier was discontinued.

## Notes for Person 3

- `notification.delivered.v1` is published directly to `notification.x` via the feedkit publisher because `core/events.ExchangeFor` does not know the type. Suggested: add `TypeNotificationDelivered = "notification.delivered.v1"` → `ExchangeNotification` there, and bind `audit.ledger` to `notification.x` so dossiers include deliveries.
- The service subscribes with `core/bus`, so queue/DLQ declaration and retry semantics are exactly yours.
