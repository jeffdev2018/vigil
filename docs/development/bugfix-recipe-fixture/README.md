# Multica bug-fix recipe fixture

Tiny Node test used by [../bugfix-recipe.md](../bugfix-recipe.md).

```bash
node prove.mjs          # reproduce fails, then fixed path passes
npm test                # fails (bug present)
npm run test:fixed      # passes (FIX_APPLIED=1)
```

No Multica server, daemon, or provider CLI is required for `prove.mjs`.
