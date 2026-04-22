/**
 * Cloudflare Worker — api.drizz.ai
 *
 * Accepts two endpoints from the drizz-farm client:
 *   POST /v1/signup     — mandatory email capture during `drizz-farm setup`
 *   POST /v1/heartbeat  — anonymous daily activity ping
 *
 * Both write to D1 (SQLite). On signup we upsert (install_id is the key);
 * on heartbeat we append to the history table AND bump `last_seen` on
 * the install. Signup without a prior record creates an anonymous stub
 * that the welcome-email verify step later ties to a human.
 *
 * Design notes:
 * - Minimal validation: the client already validated email format. We just
 *   trust-but-verify (ensure required fields are non-empty).
 * - CF-Connecting-IP and CF-IPCountry are captured from CF's own headers
 *   so we get IP + country for free, no GeoIP library.
 * - CORS wildcard — harmless because the endpoints are write-only and
 *   we never return sensitive data.
 * - No auth. The install_id itself is unguessable. If someone spams
 *   random install_ids with fake emails, add rate limiting (KV counter).
 */

import { sendWelcome, verifyURL, randomToken } from './email';

export interface Env {
  DB: D1Database;
  RESEND_API_KEY?: string;
  RESEND_FROM?: string;
  /**
   * Public base URL the verify link points at — normally the Worker's
   * custom domain (https://api.drizz.ai). Falls back to the request URL
   * if unset. Set via:
   *   npx wrangler secret put PUBLIC_BASE_URL
   */
  PUBLIC_BASE_URL?: string;
}

interface SignupBody {
  install_id: string;
  email: string;
  org_name?: string;
  hostname?: string;
  os?: string;
  arch?: string;
  version?: string;
}

interface HeartbeatBody {
  install_id: string;
  version?: string;
  os?: string;
  arch?: string;
  node_count?: number;
  sessions_today?: number;
  emulators_today?: number;
  uptime_seconds?: number;
}

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      'Content-Type': 'application/json',
      'Access-Control-Allow-Origin': '*',
    },
  });
}

function corsPreflight(): Response {
  return new Response(null, {
    status: 204,
    headers: {
      'Access-Control-Allow-Origin': '*',
      'Access-Control-Allow-Methods': 'POST, OPTIONS',
      'Access-Control-Allow-Headers': 'Content-Type',
      'Access-Control-Max-Age': '86400',
    },
  });
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);

    // Preflight
    if (request.method === 'OPTIONS') return corsPreflight();

    // Health check — useful for uptime monitoring.
    if (request.method === 'GET' && url.pathname === '/') {
      return json({ status: 'ok', service: 'drizz-farm-api' });
    }

    // Signup
    if (request.method === 'POST' && url.pathname === '/v1/signup') {
      return handleSignup(request, env);
    }

    // Heartbeat
    if (request.method === 'POST' && url.pathname === '/v1/heartbeat') {
      return handleHeartbeat(request, env);
    }

    // Verify — GET /v1/verify?token=...
    // Clicking the link in the welcome email lands here. We mark the
    // install as verified, then redirect to a friendly "thank you" page.
    if (request.method === 'GET' && url.pathname === '/v1/verify') {
      return handleVerify(request, env);
    }

    return json({ error: 'not found' }, 404);
  },
};

async function handleSignup(request: Request, env: Env): Promise<Response> {
  let body: SignupBody;
  try {
    body = await request.json();
  } catch {
    return json({ error: 'invalid json' }, 400);
  }

  if (!body.install_id || typeof body.install_id !== 'string') {
    return json({ error: 'install_id required' }, 400);
  }
  if (!body.email || !EMAIL_RE.test(body.email)) {
    return json({ error: 'valid email required' }, 400);
  }

  const ip = request.headers.get('CF-Connecting-IP') ?? '';
  const country = request.headers.get('CF-IPCountry') ?? '';
  const ua = request.headers.get('User-Agent') ?? '';

  // Upsert: if the same install_id registers twice (re-run setup after
  // wiping ~/.drizz-farm/.registered), overwrite but keep first_seen.
  // Detect whether this install already exists (so we don't email twice).
  const existing = await env.DB.prepare(
    `SELECT install_id, verified_at FROM installs WHERE install_id = ?`
  ).bind(body.install_id).first<{ install_id: string; verified_at: string | null }>();
  const isNewInstall = existing === null;

  await env.DB.prepare(`
    INSERT INTO installs (install_id, email, org_name, hostname, os, arch, version, ip, country, user_agent, last_seen)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
    ON CONFLICT(install_id) DO UPDATE SET
      email      = excluded.email,
      org_name   = excluded.org_name,
      hostname   = excluded.hostname,
      os         = excluded.os,
      arch       = excluded.arch,
      version    = excluded.version,
      ip         = excluded.ip,
      country    = excluded.country,
      user_agent = excluded.user_agent,
      last_seen  = CURRENT_TIMESTAMP
  `).bind(
    body.install_id,
    body.email,
    body.org_name ?? null,
    body.hostname ?? null,
    body.os ?? null,
    body.arch ?? null,
    body.version ?? null,
    ip,
    country,
    ua,
  ).run();

  // If this is a brand-new install (and we have Resend configured),
  // send the verify-your-email welcome message. Fire-and-forget —
  // signup succeeds even if the email send fails.
  if (isNewInstall && env.RESEND_API_KEY) {
    const token = randomToken();
    await env.DB.prepare(
      `INSERT INTO verify_tokens (token, install_id) VALUES (?, ?)`
    ).bind(token, body.install_id).run();

    const baseURL = env.PUBLIC_BASE_URL || new URL(request.url).origin;
    const link = verifyURL(baseURL, token);

    // Not awaited — the Worker runtime keeps async work alive via ctx.waitUntil
    // in production; here we just fire and don't block the response.
    void sendWelcome(env, {
      email: body.email,
      installID: body.install_id,
      orgName: body.org_name,
      verifyToken: token,
    }, link);
  }

  return json({ status: 'ok' });
}

/**
 * handleVerify is hit when the user clicks the link in the welcome email.
 * We validate the one-shot token, mark the install as verified, and
 * redirect to a friendly page. Tokens are single-use so re-clicks just
 * land on the same page without changing state.
 */
async function handleVerify(request: Request, env: Env): Promise<Response> {
  const url = new URL(request.url);
  const token = url.searchParams.get('token') ?? '';
  if (!token) return simpleHTML('Missing token', 'The link appears to be malformed.');

  const row = await env.DB.prepare(
    `SELECT install_id, used_at FROM verify_tokens WHERE token = ?`
  ).bind(token).first<{ install_id: string; used_at: string | null }>();
  if (!row) {
    return simpleHTML('Invalid token', 'This verification link is invalid or has expired.');
  }

  // Already used → idempotent success page.
  if (!row.used_at) {
    await env.DB.batch([
      env.DB.prepare(`UPDATE verify_tokens SET used_at = CURRENT_TIMESTAMP WHERE token = ?`).bind(token),
      env.DB.prepare(`UPDATE installs SET verified_at = CURRENT_TIMESTAMP WHERE install_id = ?`).bind(row.install_id),
    ]);
  }

  return simpleHTML('Email verified', 'Thanks — you\'re all set. You can close this tab and get back to drizz-farm.');
}

/** Small HTML page for verification responses (we don't use a template engine here). */
function simpleHTML(title: string, body: string): Response {
  const html = `<!DOCTYPE html><html><head><meta charset="utf-8"/>
<title>${title} — drizz-farm</title>
<style>
  body { font-family: -apple-system, sans-serif; background: #0a0a0a; color: #e8e8e8;
         display: flex; align-items: center; justify-content: center;
         min-height: 100vh; margin: 0; }
  .box { max-width: 440px; padding: 40px 32px; text-align: center; }
  h1 { font-size: 24px; margin-bottom: 12px; }
  p  { color: #888; line-height: 1.6; }
  a  { color: #22c55e; text-decoration: none; }
</style></head><body><div class="box">
<h1>${title}</h1><p>${body}</p>
<p style="margin-top:24px"><a href="https://drizz.ai">← drizz.ai</a></p>
</div></body></html>`;
  return new Response(html, { status: 200, headers: { 'Content-Type': 'text/html; charset=utf-8' } });
}

async function handleHeartbeat(request: Request, env: Env): Promise<Response> {
  let body: HeartbeatBody;
  try {
    body = await request.json();
  } catch {
    return json({ error: 'invalid json' }, 400);
  }

  if (!body.install_id || typeof body.install_id !== 'string') {
    return json({ error: 'install_id required' }, 400);
  }

  // Append the heartbeat row.
  await env.DB.prepare(`
    INSERT INTO heartbeats (install_id, version, os, arch, node_count, sessions_today, emulators_today, uptime_seconds)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?)
  `).bind(
    body.install_id,
    body.version ?? null,
    body.os ?? null,
    body.arch ?? null,
    body.node_count ?? null,
    body.sessions_today ?? null,
    body.emulators_today ?? null,
    body.uptime_seconds ?? null,
  ).run();

  // Bump last_seen on the install record, if it exists. Signup always
  // happens before heartbeats, so this should virtually never be a no-op.
  await env.DB.prepare(`
    UPDATE installs SET last_seen = CURRENT_TIMESTAMP, version = COALESCE(?, version)
    WHERE install_id = ?
  `).bind(body.version ?? null, body.install_id).run();

  return json({ status: 'ok' });
}
