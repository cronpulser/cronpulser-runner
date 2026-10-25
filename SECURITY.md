# Security policy

The CronPulser Runner executes commands on your machines, so we treat
security reports as a priority.

## Reporting a vulnerability

Email **info@cronpulser.com** with the subject "Runner security" and include
the affected version (`cronpulser-runner version`), the operating system, and
steps to reproduce. Please do not open a public GitHub issue for a
vulnerability.

We aim to acknowledge reports within three working days and to ship a fixed
release as soon as the issue is confirmed.

## Supported versions

Only the latest release receives security fixes. Releases are published at
`https://github.com/cronpulser/cronpulser-runner/releases/tag/v{version}` with a `SHA256SUMS` file
that the installers verify before running the binary.
