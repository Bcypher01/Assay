# scripts

One-off investigation tools that are not part of the build and are not run by
CI. Nothing under `internal/` or `cmd/` may depend on anything here.

- `bi5-probe.py` — dumps raw Dukascopy BI5 candle fields against the live feed,
  used to settle the decode layout in `internal/venue/dukascopy` against real
  data rather than against a fixture we encoded ourselves.
