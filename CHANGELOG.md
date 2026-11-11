# Changelog

All notable changes are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
semantic versioning once it reaches 1.0.

## [Unreleased]

### Added

- Open-source project documentation, licence and third-party notices.
- Self-hosted web fonts so the local UI works offline.

### Fixed

- Run history now restores wins, losses, net P&L, expectancy, profit factor,
  unresolved trades and unfilled orders from persisted runs.
- Web formatting checks pass again.
- Account values keep the same currency format on the server and in the
  browser, so the trading-reality screen hydrates without a mismatch.
- The setup chart opens on prepared bars instead of waiting on an unavailable
  latest partition, and says which date its history ends.
- Re-selecting the current drawing no longer discards setup test results.

## [0.1.0] — pending

First public release. Local bar preparation, strategy validation, deterministic
simulation, ambiguity ranges, run history and comparison.
