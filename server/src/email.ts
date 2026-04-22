/**
 * Welcome email via Resend (https://resend.com).
 *
 * We fire this once on first-time signup. It's best-effort — if the
 * RESEND_API_KEY secret isn't set, we just skip the send silently.
 * The install still succeeds; lead data is captured regardless.
 *
 * Set the secret with:
 *   npx wrangler secret put RESEND_API_KEY
 *   npx wrangler secret put RESEND_FROM   # e.g. "drizz-farm <welcome@drizz.ai>"
 *
 * Resend requires the "from" domain to be verified — configure drizz.ai
 * in the Resend dashboard before this will work in production.
 */

export interface EmailEnv {
  RESEND_API_KEY?: string;
  RESEND_FROM?: string;   // e.g. "drizz-farm <welcome@drizz.ai>"
}

export interface WelcomePayload {
  email: string;
  installID: string;
  orgName?: string;
  verifyToken: string;    // one-time token, appended to the verify URL
}

/** verifyURL builds the click-to-verify link users receive in the email. */
export function verifyURL(baseURL: string, token: string): string {
  return `${baseURL.replace(/\/$/, '')}/v1/verify?token=${encodeURIComponent(token)}`;
}

/**
 * sendWelcome fires a "verify your email" message to the new install.
 * Returns true on success, false on any failure. Never throws — the
 * caller treats send failures as non-fatal.
 */
export async function sendWelcome(env: EmailEnv, p: WelcomePayload, verifyLink: string): Promise<boolean> {
  if (!env.RESEND_API_KEY) return false;   // feature-gated on secret presence

  const from = env.RESEND_FROM ?? 'drizz-farm <welcome@drizz.ai>';
  const firstName = (p.orgName || '').trim().split(/\s+/)[0] || '';
  const greeting = firstName ? `Hey ${firstName} team,` : 'Hey there,';

  const text = [
    greeting,
    '',
    'Thanks for installing drizz-farm. Confirm your email so we can',
    'send you the occasional product update — no spam, easy unsubscribe.',
    '',
    `  ${verifyLink}`,
    '',
    'A few things that might help while you\'re getting started:',
    '',
    '  • Quickstart:  https://github.com/parthadrizz/drizz-farm#quickstart',
    '  • Architecture: https://github.com/parthadrizz/drizz-farm/blob/main/ARCHITECTURE.md',
    '  • Reply to this email if you hit any issues — it reaches a human.',
    '',
    '— Partha',
    'Drizz Labs',
  ].join('\n');

  const html = `
    <div style="font-family: -apple-system, Segoe UI, sans-serif; color: #222; line-height: 1.6; max-width: 520px;">
      <p>${greeting}</p>
      <p>Thanks for installing drizz-farm. Confirm your email so we can send you the occasional product update — no spam, easy unsubscribe.</p>
      <p>
        <a href="${verifyLink}"
           style="display:inline-block;background:#22c55e;color:#fff;padding:10px 20px;border-radius:6px;text-decoration:none;font-weight:500;">
          Verify email
        </a>
      </p>
      <p style="color:#666">Or paste this URL:<br><code>${verifyLink}</code></p>
      <p>A few things that might help:</p>
      <ul style="color:#444">
        <li><a href="https://github.com/parthadrizz/drizz-farm#quickstart">Quickstart</a></li>
        <li><a href="https://github.com/parthadrizz/drizz-farm/blob/main/ARCHITECTURE.md">Architecture</a></li>
        <li>Reply to this email if you hit any issues — it reaches a human.</li>
      </ul>
      <p style="color:#666">— Partha<br>Drizz Labs</p>
    </div>
  `;

  try {
    const resp = await fetch('https://api.resend.com/emails', {
      method: 'POST',
      headers: {
        'Authorization': `Bearer ${env.RESEND_API_KEY}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({
        from,
        to: [p.email],
        subject: 'Verify your email for drizz-farm',
        text,
        html,
      }),
    });
    if (!resp.ok) {
      const err = await resp.text();
      console.warn('Resend send failed:', resp.status, err);
      return false;
    }
    return true;
  } catch (e) {
    console.warn('Resend request errored:', e);
    return false;
  }
}

/** randomToken: 32 hex chars, used as a one-shot verify URL key. */
export function randomToken(): string {
  const arr = new Uint8Array(16);
  crypto.getRandomValues(arr);
  return Array.from(arr).map(b => b.toString(16).padStart(2, '0')).join('');
}
