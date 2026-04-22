# Homebrew tap setup

Users get drizz-farm via:

```bash
brew install drizz-ai/tap/drizz-farm
```

For this to work, a public GitHub repo at `github.com/drizz-ai/homebrew-tap`
must contain a `Formula/drizz-farm.rb` file. Homebrew auto-discovers taps
whose repo name starts with `homebrew-` under any user/org.

## One-time setup (when you're ready to ship v0.1.0)

1. Create the GitHub org `drizz-ai` (if it doesn't already exist).
2. Create a public repo: `drizz-ai/homebrew-tap` (exact name).
3. Inside that repo, create `Formula/drizz-farm.rb` (directory matters).
4. Add a basic `README.md`:
   ```md
   # drizz-ai/homebrew-tap

   Homebrew tap for [drizz-farm](https://drizz.ai).

       brew install drizz-ai/tap/drizz-farm
   ```
5. Leave the Formula empty for now — the next step fills it in.

## Every release

After `git tag vX.Y.Z && git push --tags` triggers the GitHub Action
that builds release artifacts:

```bash
# From the drizz-farm repo root, after `make release`:
./packaging/homebrew/update-formula.sh vX.Y.Z \
  > ~/code/drizz-ai-homebrew-tap/Formula/drizz-farm.rb

cd ~/code/drizz-ai-homebrew-tap
git add Formula/drizz-farm.rb
git commit -m "drizz-farm vX.Y.Z"
git push
```

Users now get the new version on `brew upgrade drizz-farm`.

## Testing locally before pushing

```bash
# Install the formula from the local file (no tap push needed)
brew install --formula ./packaging/homebrew/drizz-farm.rb
# ...then try it:
drizz-farm version

# Clean up:
brew uninstall drizz-farm
```

## Automating the tap update (optional, later)

When you're pushing releases frequently, add this step to the release
workflow in `.github/workflows/release.yml`:

```yaml
- name: Update Homebrew tap
  env:
    TAP_PAT: ${{ secrets.HOMEBREW_TAP_PAT }}   # fine-grained PAT with repo write on drizz-ai/homebrew-tap
  run: |
    git clone "https://x-access-token:${TAP_PAT}@github.com/drizz-ai/homebrew-tap.git" tap
    ./packaging/homebrew/update-formula.sh "$GITHUB_REF_NAME" > tap/Formula/drizz-farm.rb
    cd tap
    git config user.name  "drizz-bot"
    git config user.email "bot@drizz.ai"
    git add Formula/drizz-farm.rb
    git commit -m "drizz-farm $GITHUB_REF_NAME"
    git push
```

Skip this until you have more than a couple of manual releases under your belt.
