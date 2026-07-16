# ADR-0015: All decision records live in the API repository

- **Status:** accepted
- **Date:** 2026-07-15

## Context

EC is built across two repositories: this one (the engine and API) and
[ecv6-docs](https://github.com/mdhender/ecv6-docs) (the rulebook — the source of
truth for the rules of the game). Decisions are recorded here as ADRs under
`doc/decisions/`, numbered in one sequence, using
[the template](adr-0000-template.md). Fourteen are accepted.

Docs-first discipline (CLAUDE.md rule 3) means rules change upstream and engine
code follows. That produces decisions that are *about the rulebook* rather than
about the engine — and the natural instinct is that the docs repo should own
their records, the same way it owns the rules themselves. Deciding to split the
rulebook into a core and generator supplements ([ADR-0016](adr-0016-core-rulebook-and-generator-supplements.md))
forced the question: that decision is almost entirely about upstream prose, so
where does its record belong?

The docs repo has no ADR convention, and its structure actively resists one:

- **It publishes everything.** It is a Hugo site; every page under `content/`
  renders to the player-facing site at <https://ecv6.pbbgaming.com>. An ADR under
  `content/` would appear in the rulebook's navigation, presenting internal
  deliberation — including rejected alternatives — as if it were game material.
- **Diátaxis has no slot for it.** The four sections are tutorials, how-to,
  reference, and explanation. An ADR is none of them. *Explanation* is nearest,
  but it is scoped to design intent and setting *for players*, not to project
  meta.
- **Its own scope rule excludes it.** That repo's CLAUDE.md says not to document
  "code structure, function names, database schemas, file layouts, or framework
  choices," and asks of any candidate page: *"Does a player need this to make
  decisions or predict outcomes?"* A record of why the docs are organized a
  certain way fails that test by construction.
- **Anywhere else there is an orphan.** A `decisions/` directory outside
  `content/` would be unpublished in a repository whose entire deliverable is
  published prose — a second convention to maintain, invisible to the site.

Link direction matters too. This repo links upstream by URL; the rulebook does
not link back to tooling. A decision record living upstream that had to reference
engine consequences — frozen surfaces, seed derivation, storage — would have to
link here, inverting that.

Cross-cutting decisions make a subject-based split untenable in any case.
ADR-0016 is simultaneously a rulebook-structure decision *and* an engine decision
(a generator registry, per-generator seed roots, version pinning). Splitting
records by subject would give it two homes or an arbitrary one.

## Decision

**Every EC decision record lives in this repository, under `doc/decisions/`, in a
single sequence — including decisions about the rulebook's structure and
content.**

- **The docs repo carries rules, not decisions about rules.** It stays purely
  player- and referee-facing.
- **Where a decision changes the rulebook, the ADR records the decision and its
  rationale; the rule itself is written upstream.** This does not weaken rule 3:
  docs still lead code. The ADR is the *why*, the rulebook is the *what*, and the
  rulebook remains the source of truth for observable behavior.
- **Links stay one-directional.** ADRs may link upstream by repository URL.
  Upstream content never links back here.

## Consequences

- **One sequence, one place to look.** No split-brain numbering, and no debate
  about which repo owns a cross-cutting decision — a debate ADR-0016 would have
  started immediately.
- **The rulebook stays clean.** Deliberation, rejected alternatives, and
  implementation consequences never reach the published site. Players read rules,
  not minutes.
- **Cross-repo rationale is asymmetric, and that is the accepted cost.** The docs
  repo's own history will not explain why its structure changed; the record is
  here. A docs contributor who wants the *why* must look in this repository. We
  accept that over the alternative — publishing internal deliberation to players,
  or maintaining an unpublished orphan convention upstream.
- **Not a frozen surface.** This is an organizational convention, not part of any
  game's reproducibility contract. It can be revised without touching a stored
  game.

## Alternatives considered

- **Split by subject: docs decisions upstream, engine decisions here.** Rejected.
  It needs two sequences, and cross-cutting decisions — [ADR-0016](adr-0016-core-rulebook-and-generator-supplements.md)
  is one — have no natural home. It also requires inventing an ADR convention
  upstream that the repo's structure and scope rules both resist.
- **ADRs under the docs repo's `content/explanation`.** Rejected. It publishes
  deliberation to players as game content, and miscategorizes project meta as
  player-facing design explanation.
- **ADRs at the docs repo root, outside `content/`.** Rejected. Unpublished
  orphans in a publish-everything site, a second convention to maintain, and the
  sequence still splits.
- **A third repository for decisions.** Rejected. Overhead with no offsetting
  benefit at this size, and it separates decisions from the code whose
  constraints most of them turn on.
