# drizz-farm-api

Cloudflare Worker that handles lead capture + heartbeats for drizz-farm installs.

## What it does

| Endpoint | Purpose |
|---|---|
| `POST /v1/signup` | Store (or upsert) a new install with its email. Called once from `drizz-farm setup`. |
| `POST /v1/heartbeat` | Append a daily activity ping. Called every 24h by the running daemon. |
| `GET /` | Health check. |

Data lives in Cloudflare D1 (SQLite). Free tier handles millions of requests/day — massively over-specced for lead capture.

## Deploy

One-time setup:

```bash
cd server
npm install

# Log in to Cloudflare (opens a browser)
npx wrangler login

# Create the D1 database. Note the `database_id` it prints.
npx wrangler d1 create drizz-farm-leads

# Paste the database_id into wrangler.toml (uncomment the [[d1_databases]] block)
# Then create the schema:
npm run db:init

# Deploy the Worker
npm run deploy
```

That gives you a URL like `https://drizz-farm-api.<your-subdomain>.workers.dev`.

To use `api.drizz.ai` instead:

1. In the Cloudflare dashboard, add `drizz.ai` as a zone (if not already).
2. Uncomment the `[[routes]]` section in `wrangler.toml`.
3. `npm run deploy` again.

## Query the data

```bash
# Count of registered installs
npx wrangler d1 execute drizz-farm-leads --command "SELECT COUNT(*) FROM installs" --remote

# Installs registered in the last 24 hours
npx wrangler d1 execute drizz-farm-leads --command \
  "SELECT email, org_name, first_seen FROM installs WHERE first_seen > datetime('now', '-1 day')" --remote

# Active in the last week (heartbeating)
npx wrangler d1 execute drizz-farm-leads --command \
  "SELECT email, last_seen, version FROM installs WHERE last_seen > datetime('now', '-7 days') ORDER BY last_seen DESC" --remote

# Total emulators booted today across all installs
npx wrangler d1 execute drizz-farm-leads --command \
  "SELECT SUM(emulators_today) FROM heartbeats WHERE received_at > datetime('now', '-1 day')" --remote
```

## Local dev

```bash
npm run db:local              # init a local SQLite
npm run dev                   # runs on http://localhost:8787

# Test it
curl -X POST http://localhost:8787/v1/signup \
  -H 'Content-Type: application/json' \
  -d '{"install_id":"test-1","email":"me@example.com","version":"0.1.0"}'
```

## Point drizz-farm at the local server

```bash
DRIZZ_API_URL=http://localhost:8787 ./drizz-farm setup
```

## Cost

Cloudflare Workers free tier: 100K requests/day. D1 free tier: 5M reads/day + 100K writes/day. Unless you hit five-figure active installs, $0/mo.
