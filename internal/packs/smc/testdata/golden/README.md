# SMC annotation pilot

`pilot-fvg.json` is the fixture template for the first annotation pilot. It is
a synthetic dry run only; it is not evidence of outside validation and must not
be counted toward Batch 09's release gate.

To submit an annotated window, copy the template to a new `.json` file in this
directory and replace all of its metadata, bars, detector parameters, and
objects. Keep one file per `(window_id, annotator, primitive)` tuple. Prices
are integer ticks, time is UTC milliseconds, and `bar` is the zero-based
confirmation bar. Use `event` for a point-in-time annotation and `zone` for an
annotation with inclusive `low` and `high` bounds.

The fixture's `params` are the documented detector defaults used to score that
window. The pilot runner loads fixtures in filename order, rejects duplicate
submissions, scores every window, and produces an aggregate agreement summary
for each primitive and annotator with `smc.ScoreAll`.

Annotations should be supplied independently: do not share detector output or
another annotator's objects before submission. Record the instrument, period,
annotator identity/provenance, and regime selection outside this repository if
that data is sensitive. The 200-window, four-instrument, three-annotator
validation requirement remains pending until real submissions are available.
