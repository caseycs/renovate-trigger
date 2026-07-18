# Release automation & version consistency — research notes

> **Status:** research notes, not a decision. Date: **2026-07-18**.
> Captures primary-source findings on keeping the git tag, in-repo version
> files (`chart/Chart.yaml`), and the published artifacts (Docker image +
> OCI Helm chart) consistent while still automating releases. An ADR should
> follow once we pick a direction. Every non-obvious claim cites a primary
> source inline; URLs are collected at the bottom.

## The problem, precisely

We publish two artifacts from one repo: a multi-arch image to
`ghcr.io/caseycs/renovate-trigger:X.Y.Z` and an OCI chart to
`oci://ghcr.io/caseycs/charts/renovate-trigger`. Today `chart/Chart.yaml`
carries a placeholder `0.1.0`; the real version is stamped onto the artifacts
at publish time from the release **tag** (`helm package --version X
--app-version X`, `docker/metadata-action`).

The consistency defect is an **ordering** defect. release-drafter's model is
*tag-then-everything*: it drafts notes and resolves a version, but the git tag
does not exist until a human publishes the draft, and publishing creates the
tag on the **current** `main` commit [RD]. Any file bump (PR #10's
`version-bump` job) necessarily lands **after** that commit. So the tagged
commit's `Chart.yaml` never contains its own version — only the artifacts are
correct. The tree at tag `vX.Y.Z` and the artifact `:X.Y.Z` disagree.

This is not a release-drafter quirk to be patched; it is inherent to any flow
where the **tag is cut before the bump commit**. The fix is to reorder, not to
paper over with a follow-up commit.

## The core principle: bump → commit → tag, in that order, atomically

For the tagged commit to be self-consistent, the version bump must be committed
**first**, and the tag must be placed **on that commit**. Whichever tool you
use, evaluate it on this single axis: *does the object the tag points at
already contain the version?* Everything else (who resolves the number, how
much manual gating there is) is secondary.

A second principle follows from SemVer and from git itself: **a released
version is immutable.** SemVer §3: "Once a versioned package has been released,
the contents of that version MUST NOT be modified. Any modifications MUST be
released as a new version." [SV] git's own `git-tag` "On Re-tagging" discussion
is blunter: "Git does not (and it should not) change tags behind users back
... people MUST be able to trust their tag-names ... just call it 'X.1' and be
done with it." [GT] So "publish, then move the tag onto the bump commit" is
**not** a valid repair — it violates both. The bump has to precede the tag the
first time.

## How each tool orders bump / commit / tag

### release-please (googleapis) — bump is *inside* the tagged commit ✅

release-please maintains a long-lived **Release PR** that stays up to date as
work merges. Merging that PR is the "release now" action. On merge it: "1.
Updates your changelog file ... along with other ... files (for example
`package.json`). 2. Tags the commit with the version number. 3. Creates a
GitHub Release based on the tag." [RP-README] The tag is placed on the commit
that **already contains** the bumped files — the release commit is the merge of
the Release PR, whose branch holds the version bump [RP-tag]. This is the
bump→commit→tag ordering by construction.

For our two-artifact case, release-please can bump `chart/Chart.yaml`'s
`version` *and* `appVersion` via `extra-files` with a YAML updater, without any
language-specific release-type owning the file [RP-custom]:

```jsonc
// release-please-config.json (release-type: simple, tracks version in .release-please-manifest.json)
{
  "packages": {
    ".": {
      "release-type": "simple",
      "extra-files": [
        { "type": "yaml", "path": "chart/Chart.yaml", "jsonpath": "$.version" },
        { "type": "yaml", "path": "chart/Chart.yaml", "jsonpath": "$.appVersion" }
      ]
    }
  }
}
```

Current major is **release-please-action@v4** [RP-action]. It exposes outputs —
`release_created`, `tag_name`, `version`, `major`, `minor` — so the same
workflow can gate the build/publish job: "these `if` statements ensure that a
publication only occurs when a new release is created" via
`steps.release.outputs.release_created` [RP-action]. That is exactly the
"release on demand, then build the artifacts" shape we already have, minus the
inconsistency.

### semantic-release — same defect as release-drafter ⚠️

semantic-release runs its plugin lifecycle in one CI job:
`verifyConditions → analyzeCommits → verifyRelease → generateNotes → prepare →
publish → success/fail` [SR-plugins]. `@semantic-release/git` (which commits the
bumped `package.json`/`CHANGELOG.md` back to the repo) runs in the **prepare**
step [SR-git]. The catch: semantic-release **creates the tag on the commit that
triggered the release**, i.e. the HEAD *before* `@semantic-release/git` adds its
release commit. The maintainers document this: the tool "generates a tag on the
last commit when users would expect the tag to be set on the next commit, the
one that contains the updated files" (issue #2198) [SR-2198]. So
semantic-release + `@semantic-release/git` reproduces our exact problem: the
bump commit sits **after** the tag, untagged. Notably `@semantic-release/git`
itself carries a warning that you "likely do not need this plugin ... consider
our recommendation against making commits during your release" [SR-git] —
semantic-release is designed for ecosystems (npm) where the registry, not the
git tree, is the source of truth and nobody expects the tagged tree to carry the
number. That assumption does not hold for a Helm chart, where `Chart.yaml`
lives in the tree.

### release-drafter — drafts notes only; tag is last 🚫 (our current tool)

release-drafter "only creates/updates draft GitHub releases" with generated
notes and resolves `$RESOLVED_VERSION` from PR labels; it "does **not** bump
version files or create git tags" — "the git tag is created **only when a human
manually publishes the draft release**" [RD]. It is a notes-and-number tool, not
a versioning tool. Consistency is out of scope for it by design, which is why
PR #10 had to bolt on a post-publish bump — and why that bump can never make it
into the tagged commit.

## Comparison on the axis that matters

| Tool | Ordering | Tagged commit carries the version? | Manual gate to cut a release | Fits "commit to `main` freely" |
|---|---|---|---|---|
| **release-please** | bump → commit → **tag on it** [RP-README] | **Yes** | Merge the Release PR (one click) | Yes — PR accumulates, nothing releases until merged |
| **semantic-release** (+`/git`) | tag on trigger commit, **then** bump commit [SR-2198] | **No** (bump lands after tag) | None by default — releases every qualifying push | Weakly — needs branch discipline / `[skip ci]` |
| **release-drafter** (today) | draft → publish → **tag**, bump after [RD] | **No** | Publish the draft (one click) | Yes, but tagged tree is inconsistent |
| **chart-releaser** (chart only) | bump `Chart.yaml` in PR → merge → CI tags that commit [CR] | **Yes** (for the chart) | Merge the bump PR | Yes |

Two tools give a self-consistent tagged commit (release-please, chart-releaser);
both achieve it the same way — **the version bump is a committed change on
`main`, and the tag is derived from it.** The two that don't (semantic-release
with `/git`, release-drafter) both cut the tag before the file bump exists.

## Single source of truth: tag-derived files vs file-derived tag

Two coherent models exist; the incoherent one is "both, independently."

- **Files are the source of truth; the tag is derived.** This is the official
  Helm chart-release model. chart-releaser "check[s] each chart ... and whenever
  there's a new chart version [in `Chart.yaml`], creates a corresponding GitHub
  release named for the chart version" and tags it [CR]. You bump `Chart.yaml`
  in a normal PR; merging it is the release trigger; CI reads the file and
  creates the matching tag + release. `Chart.yaml` is authoritative — it "serves
  as the authoritative metadata source for chart information" [HELM-charts].

- **The tag is the source of truth; files are derived.** This is our current
  release-drafter flow (tag → `helm package --version`). It works for the
  *artifact* because `helm package --version/--app-version` overrides whatever
  `Chart.yaml` holds — "`--version`: set the version on the chart to this semver
  version"; "`--app-version`: set the appVersion on the chart to this version"
  [HELM-pkg] — and `helm push` derives the OCI tag from the packaged chart:
  "the registry reference basename is inferred from the chart's name, and the tag
  is inferred from the chart's semantic version" [HELM-reg]. But it leaves the
  **in-repo** `Chart.yaml` stale, which is the whole defect.

release-please unifies the two: the number is committed **into** the files and
the tag is created **from** that same commit — one write, one truth.
`Chart.yaml`'s `version` must be SemVer 2 (it "should follow the SemVer 2
standard") while `appVersion` "is not related to the version field ... This
field is informational, and has no impact on chart version calculations"
[HELM-charts]. Keeping both in the tree and bumping both keeps the checkout,
the tag, and the artifact identical.

**Recommendation on the SoT question:** make the **in-repo version the source of
truth and derive the tag from it** (the Helm-native model). It is the only model
where a plain `git checkout vX.Y.Z` is honest, and it does not depend on
`helm package --version` overrides to hide a stale file.

## GitHub mechanics that constrain the design

- **The `release`/`published` trigger is fine to keep.** "Workflows are not
  triggered for the `created`, `edited`, or `deleted` activity types for draft
  releases" [GH-events]; a workflow on `release: [published]` fires when the
  draft is published — which is also when the tag is created. That is why the
  tag-then-build ordering is baked into today's setup.
- **`GITHUB_TOKEN` pushes do not cascade.** "When you use the repository's
  `GITHUB_TOKEN` to perform tasks, events triggered by the `GITHUB_TOKEN` will
  not create a new workflow run" (with narrow exceptions) [GH-token]. Two
  consequences: (a) a bump commit pushed by CI with the default token will
  **not** re-trigger CI (no loop) — but (b) it also will **not** trigger a
  release workflow, so you cannot rely on "CI pushes a bump → that push starts
  the release." release-please sidesteps this entirely: the human merge of the
  Release PR is the trigger, and the action runs in that same run.

## The tag-moving anti-pattern (why we can't just "fix" the current flow)

The tempting patch — publish as today, then force the tag onto the bump commit —
is explicitly discouraged. git-tag: "If somebody got a release tag from you, you
cannot just change the tag for them by updating your own one. This is a big
security issue ... people MUST be able to trust their tag-names" and the
prescribed fix is a **new** name, not a moved one [GT]. SemVer §3 says the same
for the released contents [SV]. Once GHCR has `:X.Y.Z` and a consumer has pulled
tag `vX.Y.Z`, moving that tag is a silent divergence. So the ordering must be
right the **first** time; there is no compliant retro-fix.

## Recommendation for this project

Given the constraints — commit to `main` freely, release **on demand** (not on
every merge), and end up with tag ⇄ `Chart.yaml` ⇄ image ⇄ OCI chart all
agreeing — adopt **release-please (action v4)** and retire release-drafter +
the PR #10 post-publish bump:

1. **release-please maintains a Release PR** off Conventional Commits
   (`fix:` → PATCH, `feat:` → MINOR, `!`/`BREAKING CHANGE:` → MAJOR [CC], per
   SemVer [SV]). Merging to `main` never releases; the PR just accumulates. This
   preserves "commit freely, release when I decide" — the merge of the Release
   PR is the deliberate one-click "cut it now."
2. **Use `release-type: simple` + `extra-files`** to bump `chart/Chart.yaml`'s
   `version` and `appVersion` (and the manifest) **inside the Release PR**
   [RP-custom]. Merging that PR produces a commit that already holds the version,
   and release-please tags **that** commit and creates the GitHub Release
   [RP-README][RP-tag]. The tagged tree is now self-consistent.
3. **Keep the existing `release: [published]` build job** almost unchanged — it
   already reads the version from the tag and stamps both artifacts. It can stay
   tag-driven, or be folded into the release-please workflow gated on
   `release_created` [RP-action]; either way the tag it builds from now points
   at a commit whose `Chart.yaml` matches. (If folded in, note that a
   `GITHUB_TOKEN`-created release **won't** re-trigger a separate `release`
   workflow [GH-token], so run the build in the same job or use the action's
   outputs to gate it.)
4. **Stop overriding at package time as the consistency mechanism.** With
   `Chart.yaml` correct in the tree you can drop `--version/--app-version` and
   just `helm package chart` (the file is now authoritative [HELM-charts]);
   keeping the flags is harmless but no longer load-bearing.

Net effect: one number, written once into the files, committed, then tagged and
published from that commit — the bump→commit→tag principle, with a manual gate
(merge the Release PR) that matches "release on demand." This is the same shape
the official Helm flow uses (chart-releaser: bump file → merge → tag [CR]),
generalized to also cover the Docker image.

If we would rather **not** introduce Conventional Commits, the fallback is a
manually-triggered `workflow_dispatch` that (a) writes the chosen version into
`Chart.yaml`, (b) commits it, (c) creates the tag on that commit, (d) creates
the Release, and (e) builds. That reproduces release-please's ordering by hand
and keeps full manual control, at the cost of writing/maintaining the glue.
Either way, the invariant is the same: **the tag must be created on a commit
that already contains the version.**

---

## Sources (primary)

- **[RP-README]** release-please, "When the Release PR is merged ... Tags the
  commit with the version number. Creates a GitHub Release." —
  https://github.com/googleapis/release-please
- **[RP-tag]** release-please README, tag is placed on the merged Release PR
  commit (which carries the bumped files) —
  https://github.com/googleapis/release-please
- **[RP-custom]** release-please `extra-files` / generic & YAML updaters
  (`x-release-please-version`, jsonpath), `release-type: simple` —
  https://github.com/googleapis/release-please/blob/main/docs/customizing.md
- **[RP-action]** release-please-action **v4**, outputs (`release_created`,
  `tag_name`, `version`) and gating publish jobs —
  https://github.com/googleapis/release-please-action
- **[SR-plugins]** semantic-release plugin lifecycle order (prepare → publish) —
  https://semantic-release.gitbook.io/semantic-release/usage/plugins
- **[SR-git]** `@semantic-release/git` runs in `prepare`; default assets/message;
  "recommendation against making commits during your release" —
  https://github.com/semantic-release/git
- **[SR-2198]** semantic-release tags the trigger commit, not the
  `@semantic-release/git` release commit —
  https://github.com/semantic-release/semantic-release/issues/2198
- **[RD]** release-drafter drafts notes + resolves version only; does not bump
  files or create tags; tag created only on manual publish —
  https://github.com/release-drafter/release-drafter
- **[CR]** chart-releaser-action: on a new chart version, "creates a
  corresponding GitHub release named for the chart version" (file-derived tag) —
  https://github.com/helm/chart-releaser-action
- **[HELM-charts]** Chart.yaml `version` (SemVer 2) vs informational
  `appVersion`; Chart.yaml is authoritative metadata —
  https://helm.sh/docs/topics/charts/
- **[HELM-pkg]** `helm package --version` / `--app-version` override the chart's
  values at package time — https://helm.sh/docs/helm/helm_package/
- **[HELM-reg]** `helm push ... oci://...`; OCI tag inferred from the chart's
  semantic version — https://helm.sh/docs/topics/registries/
- **[GH-events]** `release` event; draft `created/edited/deleted` do not trigger
  workflows; use `published` —
  https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows
- **[GH-token]** events triggered by `GITHUB_TOKEN` do not create new workflow
  runs —
  https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow
- **[GT]** git-tag "On Re-tagging": do not move a published tag; use a new name —
  https://git-scm.com/docs/git-tag
- **[SV]** SemVer 2.0.0 §3 (released contents immutable) + increment summary —
  https://semver.org/
- **[CC]** Conventional Commits 1.0.0: `fix`→PATCH, `feat`→MINOR,
  `BREAKING CHANGE`/`!`→MAJOR — https://www.conventionalcommits.org/en/v1.0.0/
