# Bibliography

---

**Patterns for Things That Change With Time**
Martin Fowler.
[martinfowler.com/eaaDev/timeNarrative.html](https://martinfowler.com/eaaDev/timeNarrative.html)

Fowler's catalog of temporal patterns, including *Effectivity* — modelling a value as valid over a date (or, for us, a turn) range.
The clearest short introduction to the idea that "what is true" depends on "as of when."
Background for [Storing State as Timebound Facts](storing-state-as-timebound-facts.md).

---

**Developing Time-Oriented Database Applications in SQL**
Richard T. Snodgrass, Morgan Kaufmann, 2000. Available online.

The standard reference on valid-time and bitemporal modelling in SQL.
Heavier than needed for the design notes, but the authoritative treatment of period columns, as-of queries, and the half-open interval convention.

---

**Temporal Features in SQL:2011**
Krishna Kulkarni and Jan-Eike Michels, *ACM SIGMOD Record* 41(3), 2012.

The concise summary of how application-time periods and system-versioned tables entered the SQL standard.
Useful for the conventional vocabulary (`valid_from` / `valid_to`, `PERIOD`, `AS OF`) that the timebound-fact design mirrors.
