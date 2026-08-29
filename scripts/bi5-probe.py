#!/usr/bin/env python3
"""Dump raw Dukascopy candle BI5 fields so the decode layout can be settled
against real data instead of against a fixture we encoded ourselves.

Run this on a machine with network access to datafeed.dukascopy.com:

    python3 bin/bi5-probe.py                       # EURUSD 2024 daily
    python3 bin/bi5-probe.py --symbol GBPUSD --year 2025
    python3 bin/bi5-probe.py --minute 2024-06-03   # one day of M1

It prints each 24-byte record decoded under the layout internal/venue/dukascopy
assumes (time, open, close, low, high, volume) plus the raw uint32s, so a
mismatch is visible without trusting the assumption.
"""
import argparse, datetime as dt, lzma, struct, sys, time, urllib.error, urllib.request

BASES = (
    "https://datafeed.dukascopy.com/datafeed",
    "https://www.dukascopy.com/datafeed",
)
REC = 24


def fetch(path):
    error = None
    for attempt in range(6):
        url = f"{BASES[attempt % len(BASES)]}/{path}"
        req = urllib.request.Request(url, headers={"User-Agent": "assay-bi5-probe"})
        try:
            with urllib.request.urlopen(req, timeout=30) as r:
                return url, r.read()
        except (OSError, urllib.error.HTTPError) as exc:
            error = exc
            if isinstance(exc, urllib.error.HTTPError) and exc.code == 404:
                raise
            time.sleep(min(2 ** attempt, 8))
    raise error


def decompress(blob):
    if not blob:
        return b""
    # Dukascopy ships raw LZMA (alone) streams with an unknown size marker.
    d = lzma.LZMADecompressor(format=lzma.FORMAT_ALONE)
    return d.decompress(blob)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--symbol", default="EURUSD")
    ap.add_argument("--year", type=int, default=2024)
    ap.add_argument("--minute", help="YYYY-MM-DD: probe that day's M1 file instead of the annual daily file")
    ap.add_argument("--hour", help="YYYY-MM: probe that month's H1 file instead of the annual daily file")
    ap.add_argument("--limit", type=int, default=12)
    ap.add_argument("--scale", type=float, help="tick size for display; inferred from the symbol by default")
    ap.add_argument("--hex", action="store_true", help="print each displayed record as captured wire hex")
    a = ap.parse_args()

    if a.minute and a.hour:
        ap.error("--minute and --hour are mutually exclusive")
    if a.minute:
        day = dt.date.fromisoformat(a.minute)
        path = f"{a.symbol}/{day.year:04d}/{day.month - 1:02d}/{day.day:02d}/BID_candles_min_1.bi5"
        base = dt.datetime(day.year, day.month, day.day, tzinfo=dt.timezone.utc)
    elif a.hour:
        month = dt.date.fromisoformat(f"{a.hour}-01")
        path = f"{a.symbol}/{month.year:04d}/{month.month - 1:02d}/BID_candles_hour_1.bi5"
        base = dt.datetime(month.year, month.month, 1, tzinfo=dt.timezone.utc)
    else:
        path = f"{a.symbol}/{a.year:04d}/BID_candles_day_1.bi5"
        base = dt.datetime(a.year, 1, 1, tzinfo=dt.timezone.utc)

    url, blob = fetch(path)
    print(f"GET {url}", file=sys.stderr)
    raw = decompress(blob)
    n = len(raw) // REC
    scale = a.scale if a.scale is not None else (
        1e-3 if a.symbol.endswith("JPY") or a.symbol.startswith(("XAU", "XAG")) else 1e-5
    )
    print(f"{len(raw)} bytes -> {n} records of {REC} bytes "
          f"({'exact' if len(raw) % REC == 0 else 'RAGGED — record size is wrong'})\n")

    print(f"{'timestamp':<22} {'f1':>9} {'f2':>9} {'f3':>9} {'f4':>9} {'vol':>10}   as o/c/l/h")
    for i in range(min(n, a.limit)):
        off, f1, f2, f3, f4, vol = struct.unpack(">Iiiiif", raw[i * REC:(i + 1) * REC])
        ts = base + dt.timedelta(seconds=off)
        o, c, l, h = f1 * scale, f2 * scale, f3 * scale, f4 * scale
        flag = "" if (l <= o <= h and l <= c <= h) else "   <-- o/c outside l..h"
        same = "  O==C" if f1 == f2 else ""
        print(f"{ts:%Y-%m-%d %a %H:%M}  {f1:>9} {f2:>9} {f3:>9} {f4:>9} {vol:>10.2f}   "
              f"o={o:.5f} c={c:.5f} l={l:.5f} h={h:.5f}{flag}{same}")
        if a.hex:
            print(f"  hex={raw[i * REC:(i + 1) * REC].hex()}")

    ident = sum(1 for i in range(n)
                if struct.unpack_from(">i", raw, i * REC + 4)[0]
                == struct.unpack_from(">i", raw, i * REC + 8)[0])
    print(f"\nrecords where field1 == field2 (open == close): {ident}/{n}")
    print("Flat and closed-market records can legitimately have open == close; "
          "inspect active records and volume before diagnosing the layout.")

if __name__ == "__main__":
    main()
