# n8n workflows

Lightweight automation glue (PRD §7 P1.E). Import via n8n → Workflows → Import from file.

| Workflow | Trigger | What it does |
|---|---|---|
| `service-health-check.json` | every 5 min | GETs `/healthz` on every Sotería service; any non-200 (a stale feed, a lost broker, three failed polls…) posts the JSON body to Slack |

Setup:

1. Run n8n (free, self-hosted): `docker run -p 5678:5678 -e SLACK_WEBHOOK_URL=https://hooks.slack.com/... n8nio/n8n`
2. Import the workflow. The service URLs use compose service names (`http://ingestion-fda:8080/healthz`); when n8n runs outside the compose network, edit the **Service list** node to `http://localhost:<port>/healthz` (ports: fda/usda/rasff 8080, resolution 8081, containment 8082, rescue 8083, notification 8084).
3. Activate the workflow.

The health semantics being probed are documented in `libs/feedkit/README.md` (ingestion) and each service's README.
