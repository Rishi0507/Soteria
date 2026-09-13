# infra

Everything needed to run Sotería as a system rather than as a set of processes.

```sh
cd infra
docker compose up -d            # broker + the four domain services
docker compose logs -f rabbitmq # watch the topology import
```

| Port | What |
|---|---|
| 5672 | AMQP |
| 15672 | RabbitMQ management UI (credentials from `infra/.env`) |
| 8081–8084 | resolution, containment, order rescue, audit |

## The topology is declarative

`rabbitmq/definitions.json` is imported at boot, so every exchange, queue,
dead-letter pair and binding exists before any service connects. Services still
declare their own queues idempotently on startup, which means a service can be
deployed without an infra change — and it means the two declarations can drift.

`rabbitmq/topology_test.go` compares three sources that must agree: the registry
in `contracts/rabbitmq-topology.md`, these definitions, and what the services
actually subscribe to. It also enforces the properties the audit trail depends
on: every consumer queue is durable, has a dead-letter exchange, and has a bound
`.dlq` to catch what lands there; and `audit.ledger` is bound to every exchange
that carries incident events, because a dossier assembled from a subset is
evidence of nothing.

```sh
cd infra && go test ./...      # no broker needed
```

## Proving it against a real broker

The in-process bus implements the same routing rules, but cannot tell you whether
queues really get declared, whether bindings match the keys services publish on,
or whether a failing handler actually dead-letters. `tests/integration` does:

```sh
docker compose up -d rabbitmq
RABBITMQ_URL=amqp://$RABBITMQ_USER:$RABBITMQ_PASSWORD@localhost:5672/ go test ./tests/integration/...
```

Without `RABBITMQ_URL` those tests skip, so CI without a broker stays green.

## Credentials

Nothing here touches a real store or sends a real message unless you supply
credentials. Copy `.env.example` to `infra/.env` and set what you need:

| Variable | Effect when unset |
|---|---|
| `RABBITMQ_USER`, `RABBITMQ_PASSWORD` | a development account is created; fine on a laptop, never elsewhere |
| `SHOPIFY_SHOP`, `SHOPIFY_ACCESS_TOKEN` | services run on bundled fixtures and log that their holds are not real |
| `CONSENT_SECRET` | order rescue starts with a development secret and warns |
| `TSA_URL` | dossiers are timestamped by DigiCert's public authority |
| `TSA_DISABLED=1` | dossiers are hash-chained but not anchored in time |

Setting only one half of a Shopify pair is a startup failure, not a silent
downgrade: a service that believes it is writing to a store while running on
fixtures would publish containment events describing holds that never happened.

Before deploying anywhere shared, read `docs/security-review.md`. The defaults
here are chosen for a laptop.
