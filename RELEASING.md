# Releasing

Every push to `main` runs the `release` workflow, which builds all artifacts
(archives, deb, rpm) and keeps a **draft release** on GitHub up to date.

To release, open [Releases](https://github.com/servak/mping/releases), check
the draft and press **Publish release**. GitHub creates the tag on the commit
the draft was built from; nothing else needs to run.

Release notes are GitHub's generated notes (merged PRs with `@author`, plus
new contributors), produced before the tag exists.

## Version

The draft's version is derived from [Conventional Commits](https://www.conventionalcommits.org/)
since the latest `vX.Y.Z` tag (`.github/scripts/next-version.sh`):

| Commits since last tag            | Bump                       |
|-----------------------------------|----------------------------|
| `feat!:` / `BREAKING CHANGE:`     | major (minor while on v0)  |
| `feat:`                           | minor                      |
| `fix:` / `perf:`                  | patch                      |
| only `docs:`, `ci:`, `chore:`, …  | no draft is made           |

To use a different version, run the `release` workflow manually
(Actions → release → Run workflow) with `version`, e.g. `v1.0.0`. It replaces
the existing draft.
