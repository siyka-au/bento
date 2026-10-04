# CLAUDE.md

This is a fork of `warpstreamlabs/bento`. The `upstream` remote tracks the
original project and `origin` is `siyka-au/bento`.

## Workflow

- Work on feature branches off `main`. Do not commit directly to `main`.
- Use Conventional Commits.
- Add a `CHANGELOG.md` entry for any user-visible change. Sections are titled by
  topic (`[fork] ...`), not by version.
- Before opening a PR, run `make docs`, `make lint` and `make test`.

## Related repos

`go-ads` (protocol library, slog-native) and `bento-ads` (a Bento input plugin
that consumes go-ads through a local `replace` directive) sit above this fork in
a layered stack.
