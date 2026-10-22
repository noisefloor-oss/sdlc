# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately via **GitHub Security Advisories**
("Report a vulnerability" under this repository's Security tab). Do not open a
public issue for a security problem.

Reports are read by the maintainer. This is an experimental, solo-maintained
project: there is no response-time SLA and no bug bounty, but genuine
vulnerabilities are taken seriously and fixes are prioritized over feature
work.

## Scope

The trust boundary for this tool is described in the README: it does not start
harness sessions or call models, handles no credentials, and makes no network
connections itself except loopback on explicit request. `inventory`, `plan`,
`adopt`, `execution plan`, and `web` (at startup) execute installed harness
binaries for the catalog's version and model-list probes, including
discovery-candidate version probes, with five- or ten-second timeouts.
`plan`, `adopt`, `execution plan`, and `web` (on each plan/adoption request)
run `git rev-parse HEAD` in targets containing `.git` for a read-only revision
lookup, with no explicit timeout. `diagnose` runs `noise tasks version`,
`noise tasks context --json`, and `noise tasks verify --json` with five-second
timeouts. All run in the target project; those binaries may perform their own
filesystem or network I/O. Reports about behavior outside that stated
boundary (for example, issues that require an already-compromised host) are
still welcome but may be classified as hardening suggestions rather than
vulnerabilities.

## Supported versions

Only the latest tagged release is supported. There are no security backports
to earlier releases.
