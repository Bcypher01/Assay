# Security policy

## Reporting a vulnerability

Please do not open a public issue for a security problem. Use GitHub's
[private vulnerability reporting](https://github.com/Bcypher01/Assay/security/advisories/new)
for this repository. Include the affected version, a short description, and a
reproduction if you have one. We will acknowledge the report and coordinate a
fix and disclosure.

## Scope

Assay is a local-first desktop tool. It starts an HTTP server on loopback and
stores data in the user's cache and data directories. The server currently has
no authentication, so it must not be exposed to an untrusted network. Do not
bind it to `0.0.0.0` on a shared host.

Market-data credentials belong in environment variables or `deploy/.env`; they
must never be committed. The R2 publisher is the only component that needs
write credentials, and the research client never receives them.

## Supported versions

Security fixes are made on the default branch and in the latest tagged release.
There is no long-term support policy yet.
