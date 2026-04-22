# site/

Landing page for [drizz.ai](https://drizz.ai). Pure static HTML + inline CSS — no build step.

## Deploy to Cloudflare Pages

```bash
# one-time: install wrangler if you don't have it
npm install -g wrangler

# from the repo root
cd site
npx wrangler pages deploy . --project-name=drizz-ai
```

First deploy creates the Pages project. Follow the CLI prompts. After that, subsequent `npx wrangler pages deploy .` pushes an update.

To use the custom domain `drizz.ai`, go to the Cloudflare dashboard → Pages → drizz-ai project → Custom domains → add `drizz.ai`.

## Or: deploy from GitHub (automatic)

Cloudflare Pages can watch this repo and auto-deploy on push to `main`:

1. Cloudflare dashboard → Pages → Create project → Connect to Git
2. Select the drizz-farm repo
3. Build settings:
   - **Framework preset:** None
   - **Build command:** (leave empty)
   - **Build output directory:** `site`
4. Deploy

Now every push to main updates drizz.ai.

## Updating

Edit `index.html`. The styles are inline so there's nothing to build. Preview locally with:

```bash
cd site && python3 -m http.server 8000
# http://localhost:8000
```

## What about `get.drizz.ai`?

That's a separate thing — a URL that serves the `install.sh` script for `curl -fsSL https://get.drizz.ai | bash`. Options:

- **Cloudflare Pages redirect to GitHub raw** — edit `_redirects`
- **Cloudflare Worker** — tiny Worker that fetches from GitHub and returns the file
- **GitHub Pages** — point `get.drizz.ai` at `drizz-farm.github.io` and serve `install.sh`

Easiest: put `install.sh` in a separate Pages project at `get.drizz.ai`. Two clicks in the Cloudflare dashboard.
