# Counter-based PRNGs — why determinism looks the way it does

This page is *about* how the engine draws random numbers — the reasoning, the
prior art, and the trade-offs. The mechanism itself (master seeds, streams, key
paths, domain tags, the SHA-256 → PCG derivation) is specified in
[determinism.md](determinism.md); the player-facing promise lives in the docs
repo. Here we explain *why* the design takes the shape it does.

## Two demands ordinary randomness can't meet

A conventional RNG has hidden state: each call advances a sequence, and the number
you get depends on how many calls came before. That is fine for a card game, but
the engine needs two things at once that it cannot provide.

The first is **reproducibility**: given the same seeds, the world must come out
identical, every time, on any machine. The second, less obvious, is **order
independence**: we want to ask "what are the contents of the system at `(q, r)`?"
and get the same answer no matter what the engine did first, or in what order it
visits systems. A single advancing sequence entangles everything — draw stars and
then planets from it, and adding one deposit roll shifts every later roll; iterate
systems in a different order and the whole map changes.

## A stream is a hash of its address

The way out is to stop treating randomness as a sequence you consume and start
treating it as a *function you query*. Give every draw an **address**, and derive
a private stream by hashing that address together with the master seeds:

    stream = PRNG( hash(seed1, seed2, address) )

Because the stream is a pure function of the seeds and the address, *when* and *in
what order* you compute it stop mattering. The system at `(q, r)` has one address,
so one stream, so one set of contents — fixed forever for a given pair of seeds.
Order independence is not something we carefully maintain; it falls out of the
construction, because nothing in the address depends on iteration order. This is
exactly why a system's contents are drawn per-system rather than from one sweep
around the cluster.

## This is a known construction

We did not invent this, and the prior art carries hard-won lessons. The same idea
appears, under different names, across scientific computing and machine learning:

- **Counter-based RNGs.** A stateless function of a *key* and a *counter* whose
  output is a deterministic, well-mixed hash of the two. Random123 (Philox,
  Threefry) popularized these for massively parallel simulation, precisely because
  a `(key, counter)` pair can be evaluated anywhere, in any order, with no shared
  state.
- **Spawn keys.** NumPy's `SeedSequence` addresses independent streams by a
  *spawn key* — a tuple of integers naming a node in a tree of generators — which
  it hashes with the seed. Our address is a spawn key by another name.
- **Fold-in / split.** JAX threads an explicit key and derives child keys by
  *folding in* an integer. Same move: mix an identifier into a key to get an
  independent stream.
- **Domain separation.** The cryptographic practice of prefixing a distinct label
  so different uses of one hash can never collide. Our rule that each address
  begins with a purpose tag is domain separation, plain and simple.

Recognizing the design as a counter-based, spawn-keyed construction tells us the
shape is sound, lends a vocabulary, and validates choices we might otherwise
second-guess.

## Addressing: key paths and domain tags

An address is a **key path**: a short, flat sequence of integers whose first
element is a **domain tag** naming the purpose of the draw, with the remaining
elements identifying the specific instance (a system uses its `(q, r)`).

An early draft split this into a string "key" for the purpose and an integer
"leaf" for the instance. Collapsing both into one integer type felt like a hack —
until the literature made clear they were never two different things. A spawn key
is just a path of integers; whether an element names a subsystem or a coordinate
is a matter of *convention and position*, not of type. So a single `Key` type,
with coordinates coerced into it, is not an abuse of the model — it *is* the
model. The honest cost: the compiler no longer distinguishes a purpose from a
coordinate, so the discipline that keeps streams apart lives in convention, not in
types.

## Why the streams stay independent

Two draws that share an address share a stream, producing *correlated*
randomness — a quiet, corrosive class of bug. Three properties keep addresses
apart, each easier to trust seen as domain separation over a hashed path:

- **Distinct domain tags.** Every address leads with a purpose constant from one
  enumerated set, so two purposes diverge at the first element. Reserving `0` as
  invalid turns a forgotten tag into an obvious bug rather than a silent alias.
- **Unique instances within a domain.** Inside a purpose, the trailing elements
  must single out the instance (a system's `(q, r)` is unique per system). A
  purpose whose addresses were ambiguous would collide with itself.
- **Length is part of the address.** `[K, q]` and `[K, q, r]` must not hash
  alike, so the construction hashes the *number* of elements, not only their
  values — the same care NumPy takes to keep different depths of the spawn tree
  distinct.

## An address is a frozen contract

Once a game exists, its outcomes are welded to the exact addresses that produced
them. The numeric values of the domain tags, the order of the trailing elements,
the way a coordinate becomes an integer — all of it is now part of the game's
compatibility surface, no less than a save-file format.

That makes the domain-tag enumeration **append-only**. Insert a constant in the
middle and `iota` renumbers everything after it, silently rewriting every existing
world. Change how an address is built and the same seeds diverge. This is not a
rule we impose for tidiness; it is a fact about hashing an address. See
[determinism.md](determinism.md) for the operational rules this implies.

## The choices we made, and what we gave up

We **separate the hashing from the generator**. The address is hashed to derive
seed material, and that material seeds a PCG generator that produces the actual
numbers. This mirrors NumPy's deliberate split between `SeedSequence` and its bit
generators: the addressing scheme and the generator can each change without
disturbing the other.

We hash with **SHA-256**, heavier than necessary. The counter-based libraries use
fast, purpose-built mixers (Threefry, Philox) because their workloads draw
billions of numbers. Ours draws thousands, so the cost is invisible, and a
cryptographic hash buys certainty about mixing quality with no analysis on our
part. We chose simplicity and confidence over speed — a conscious trade, revisited
only if profiling ever demands it.

## What we are deliberately leaving open

The framework — a hashed key path, a leading domain tag, unique instance
addresses, length included, append-only and frozen — is enough to add any future
stream safely. What it does *not* do is decide, in advance, how each future
subsystem should shape its instance addresses. What identifies a deposit roll, a
missile attack, a turn? We answer those as the subsystems arrive and we see how
their addresses actually want to be built.

## See also

- [determinism.md](determinism.md) — the mechanism this reasoning implements
- [decisions/adr-0001-counter-based-prng.md](decisions/adr-0001-counter-based-prng.md) — the decision record
- [Random123: counter-based RNGs (D.E. Shaw Research)](https://github.com/DEShawResearch/random123)
- [NumPy SeedSequence & parallel generation (spawn / spawn_key)](https://numpy.org/doc/stable/reference/random/parallel.html)
- [JAX jax.random (fold_in / split)](https://docs.jax.dev/en/latest/jax.random.html)
