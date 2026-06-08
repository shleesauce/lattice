# Security Policy

## Supported versions

Lattice is pre-1.0. Security fixes land on `master` and ship in the latest tagged
release; please run the most recent release.

| Version | Supported |
| ------- | --------- |
| latest release (`v0.1.x`) | ✅ |
| older / `master` (unreleased) | best-effort |

## Reporting a vulnerability

**Please do not open a public issue for security vulnerabilities.**

Report privately via GitHub's **"Report a vulnerability"** button on this repository's
**Security** tab (GitHub private vulnerability reporting). That opens a confidential
advisory visible only to you and the maintainers.

Include what you found, the impact, and steps to reproduce. You'll get a best-effort
acknowledgement; once a fix is available we'll coordinate disclosure and credit you if
you'd like.

## Threat model (what to keep in mind)

Lattice is a **single-owner, self-hosted** tool. Its security rests on a few assumptions —
issues that violate these are in scope:

- **The network is the boundary.** Lattice serves plain HTTP and relies on your private
  Tailscale tailnet (WireGuard) for transport security. It is not designed to be exposed
  directly to the public internet. A configured hub refuses to bind a public address
  without an admin password (`--insecure-no-auth` is an explicit opt-out for trusted
  networks).
- **The hub is trusted by its agents.** An agent runs commands and opens PTYs/editors on
  its host on the hub's behalf — the enrollment token and admin password are the
  credentials that gate this. Token/auth bypass, privilege escalation, or a way for one
  enrolled machine to act as another are all in scope.
- **Secrets live only on the installed machine.** The master/enrollment token and config
  live under `~/.lattice` on each host and are passed to processes via the `LATTICE_TOKEN`
  environment (never on the command line). Leaks of these via the code are in scope.

Reports that amount to "the tailnet/admin password was compromised" or "an authorized user
can run commands" (that's the intended function) are generally out of scope.
