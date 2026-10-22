# CronPulser Runner

The CronPulser Runner is an independent Go module that executes approved local
scripts on Linux and Windows Server machines. It opens an outbound authenticated
WebSocket connection to CronPulser over port 443; no inbound port, public IP
address, SSH credential, or remote shell is required.

CronPulser owns scheduling. The runner executes only job keys registered in its
local YAML configuration and returns lifecycle events, stdout, stderr, exit
code, and duration.

Current release: `0.3.5`. Documentation: https://cronpulser.com/docs/runner/

## Build locally

From the repository root (requires Go 1.26 or later; `go.mod` pins the
toolchain used for releases):

```bash
VERSION=0.3.5 ./scripts/build.sh
```

This creates:

```text
dist/cronpulser-runner-linux-amd64
dist/cronpulser-runner-linux-arm64
dist/cronpulser-runner-windows-amd64.exe
```

Choose the binary matching `uname -m`:

```text
x86_64  -> cronpulser-runner-linux-amd64
aarch64 -> cronpulser-runner-linux-arm64
Windows Server AMD64 -> cronpulser-runner-windows-amd64.exe
```

## Install on Linux

Build for the current machine, then run:

```bash
sudo ./dist/cronpulser-runner-linux-amd64 install \
  --server https://api.cronpulser.com \
  --token cp_runner_xxxxxxxxx
```

The one-time token is generated from **Runners → Add Runner** in the CronPulser
UI. Installation creates the `cronpulser` system user, writes configuration
under `/etc/cronpulser/`, installs the binary at
`/usr/local/bin/cronpulser-runner`, installs and enables the systemd unit, and
starts the service.

For development, the shell installer can use a local binary without a download host:

```bash
./install-runner.sh \
  --server https://api.cronpulser.com \
  --token cp_runner_xxxxxxxxx \
  --binary-path ./dist/cronpulser-runner-linux-amd64
```

The public installer downloads the selected version from this repository's
GitHub Releases (`releases/download/v{version}/`); versions 0.3.1 and 0.3.2 are
served from `https://downloads.cronpulser.com/runner/{version}/`. It downloads
the release's `SHA256SUMS` file and refuses to execute the binary unless its
SHA-256 digest matches. The customer command is:

```bash
curl -fsSL https://downloads.cronpulser.com/runner/install-runner.sh | \
sudo sh -s -- \
  --server https://api.cronpulser.com \
  --token cp_runner_xxxxxxxxx \
  --version 0.3.5
```

## Install on Windows Server

Windows Server 2019, 2022, and 2025 on AMD64 are supported. Open PowerShell as
Administrator and run:

```powershell
Invoke-WebRequest https://downloads.cronpulser.com/runner/install-runner.ps1 -OutFile .\install-runner.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install-runner.ps1 `
  -Server https://api.cronpulser.com `
  -Token cp_runner_xxxxxxxxx `
  -Version 0.3.5
```

The installer verifies the release checksum, installs the binary under
`C:\Program Files\CronPulser`, writes state and configuration under
`C:\ProgramData\CronPulser`, and starts the automatic `CronPulserRunner`
Windows service as `LocalService`. No inbound firewall rule is required.

For a locally built binary, pass
`-BinaryPath .\dist\cronpulser-runner-windows-amd64.exe` to the PowerShell
installer.

## Releases

From 0.3.3 on, releases are built by GitHub Actions from a `v*` tag
(`.github/workflows/release.yml`). The workflow runs the tests, builds the
three binaries, writes `SHA256SUMS`, records build provenance, and publishes
everything with both installers as a GitHub Release.

Verify that a downloaded binary was built by this repository's workflow:

```bash
gh attestation verify cronpulser-runner-linux-amd64 --repo cronpulser/cronpulser-runner
```

To cut a release, set the new default version in `install-runner.sh` and
`install-runner.ps1` (the workflow refuses a tag that does not match), then
push the tag `v{version}`.

To package a release locally instead:

```bash
VERSION=0.3.5 ./scripts/build.sh
./scripts/package-release.sh 0.3.5 /tmp/cronpulser-downloads
```

The packaging script refuses to replace an existing version directory.

## Configure jobs

Edit `/etc/cronpulser/runner.yaml` as root (`sudo nano /etc/cronpulser/runner.yaml`);
on Windows, edit `C:\ProgramData\CronPulser\runner.yaml` from an Administrator
session. A fresh installation contains `jobs: {}`: replace that line with your
jobs instead of adding a second `jobs:` key. A step-by-step first test job for
Linux and Windows is in the
[setup guide](https://cronpulser.com/docs/runner/#first-job-linux).


```yaml
server: https://api.cronpulser.com
runnerId: generated-during-install
runnerSecret: generated-during-install
maxConcurrentJobs: 1
cachePath: /var/lib/cronpulser/executions.json
jobs:
  hello:
    command: /opt/cronpulser/scripts/hello.sh
    workingDirectory: /opt/cronpulser/scripts
    timeout: 30
```

Commands and arguments are stored separately and are executed without a shell.
For example, a Windows PowerShell job is configured as:

```yaml
jobs:
  backup:
    command: C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe
    arguments:
      - -NoProfile
      - -NonInteractive
      - -File
      - C:\ProgramData\CronPulser\scripts\backup.ps1
    workingDirectory: C:\ProgramData\CronPulser\scripts
    timeout: 5m
```

Create the scripts folder so only Administrators and SYSTEM can change it and
`LOCAL SERVICE` can read and run it (a folder directly under `C:\` lets every
signed-in user change its files):

```powershell
icacls C:\ProgramData\CronPulser\scripts /inheritance:r /grant:r "*S-1-5-32-544:(OI)(CI)F" "*S-1-5-18:(OI)(CI)F" "*S-1-5-19:(OI)(CI)RX"
```
 Keeping arguments separate avoids command-shell parsing and quoting
ambiguities.

Then validate and restart:

```bash
sudo -u cronpulser /usr/local/bin/cronpulser-runner validate
sudo systemctl restart cronpulser-runner
journalctl -u cronpulser-runner -f
```

After restart, the job key appears as a discovered job on the Runners page. A
CronPulser job can select that runner and job key. Both **Run Now** and normal
cron schedules dispatch through the same WebSocket connection.

If the runner is offline when a scheduled run is due, CronPulser records a
failed execution immediately. The failure is evaluated by the existing alert
pipeline. The current release does not queue missed executions for later.

A job that is already running keeps running if the connection drops. Its
result is stored locally and sent once the runner reconnects, exactly once.
If the runner itself restarts during a job, that execution is reported as
failed after the restart.

## Upgrade

Upgrading keeps the registration, credentials, `runner.yaml` jobs and the
execution cache. No token is needed. The installer downloads and verifies the
release, then the new binary runs `upgrade`:

```bash
curl -fsSL https://downloads.cronpulser.com/runner/install-runner.sh | \
sudo sh -s -- --upgrade --version 0.3.5
```

```powershell
Invoke-WebRequest https://downloads.cronpulser.com/runner/install-runner.ps1 -OutFile .\install-runner.ps1
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\install-runner.ps1 -Upgrade -Version 0.3.5
```

`upgrade` first loads the existing `runner.yaml` with the new version and
stops without changes if it is not valid. It keeps the current binary as
`cronpulser-runner.previous` (`cronpulser-runner.previous.exe` on Windows),
replaces it, and restarts the service; on Windows the service is stopped
during the swap because a running executable cannot be replaced. If the
service does not stay running for six seconds, the previous binary is restored
and restarted. A job that is running during the restart is reported as failed,
so upgrade between runs. The Runner page shows the new version once it
reconnects.

## Rotate credentials

Generate a one-time rotation token from the Runners page, then apply it on the
registered machine. Existing local jobs and execution history are preserved.

```bash
sudo cronpulser-runner rotate --token cp_runner_xxxxxxxxx
```

Rotation immediately revokes the old permanent secret. The command updates
only the credential in `/etc/cronpulser/runner.yaml`, preserves local jobs and
the persistent duplicate-execution cache, and restarts the systemd service.

## Operations

```bash
sudo systemctl status cronpulser-runner --no-pager
sudo journalctl -u cronpulser-runner -f
sudo -u cronpulser /usr/local/bin/cronpulser-runner validate
/usr/local/bin/cronpulser-runner version
```

On Windows Server, use an elevated PowerShell terminal:

```powershell
Get-Service CronPulserRunner
Get-WinEvent -LogName Application | Where-Object ProviderName -eq CronPulserRunner
& 'C:\Program Files\CronPulser\cronpulser-runner.exe' validate
& 'C:\Program Files\CronPulser\cronpulser-runner.exe' version
```

Credential rotation is the same operation on Windows:

```powershell
& 'C:\Program Files\CronPulser\cronpulser-runner.exe' rotate -token cp_runner_xxxxxxxxx
```

The duplicate-execution cache is stored at
`/var/lib/cronpulser/executions.json` on Linux and
`C:\ProgramData\CronPulser\state\executions.json` on Windows. It survives
service and machine restarts.

## Uninstall

In CronPulser, delete or reassign the jobs that use the Runner (a Runner with
scheduled jobs cannot be deleted), then delete the Runner, which revokes its
credential. Then remove it from the machine. Your own scripts are not touched.

Linux:

```bash
sudo systemctl disable --now cronpulser-runner
sudo rm -f /etc/systemd/system/cronpulser-runner.service /usr/local/bin/cronpulser-runner /usr/local/bin/cronpulser-runner.previous
sudo systemctl daemon-reload
sudo rm -rf /etc/cronpulser /var/lib/cronpulser
sudo userdel cronpulser
```

Windows Server (PowerShell as Administrator):

```powershell
Stop-Service CronPulserRunner
sc.exe delete CronPulserRunner
Remove-EventLog -Source CronPulserRunner
Remove-Item -Recurse -Force 'C:\Program Files\CronPulser', 'C:\ProgramData\CronPulser'
```

This also deletes `C:\ProgramData\CronPulser\scripts`; copy your scripts
elsewhere first to keep them.

Running the installer again without `--upgrade` does not upgrade a Runner:
on Linux it registers a new Runner and rewrites `runner.yaml`, on Windows it
stops because the service exists. Use [Upgrade](#upgrade) instead.

## Security boundaries

- Only absolute, executable paths registered in `runner.yaml` can run.
- The service runs as the unprivileged `cronpulser` system user on Linux and
  `LocalService` on Windows.
- On Linux, `runner.yaml` (which holds the permanent secret and the job
  allowlist) is owned by root and readable only by the `cronpulser` group
  (mode `0640`), so jobs running as the service account cannot add commands.
  Installations made before 0.3.2 can apply the same ownership with
  `sudo chown root:cronpulser /etc/cronpulser/runner.yaml && sudo chmod 0640 /etc/cronpulser/runner.yaml`.
- On Windows, the configuration directory is restricted to SYSTEM and
  Administrators, with read access for `LocalService`, before the secret is
  written.
- Output from each stream is limited to 1 MiB.
- Every job requires a positive timeout.
- Do not grant broad passwordless sudo access. If a script needs privilege,
  allow only that exact script in sudoers.

## Reporting security issues

See [SECURITY.md](SECURITY.md).

## License

MIT. See [LICENSE](LICENSE).
