[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string] $Server,

    [Parameter(Mandatory = $true)]
    [string] $Token,

    [string] $Version = $(if ($env:CRONPULSER_RUNNER_VERSION) { $env:CRONPULSER_RUNNER_VERSION } else { "0.3.3" }),
    [string] $DownloadBaseUrl = $env:CRONPULSER_DOWNLOAD_BASE_URL,
    [string] $BinaryPath
)

$ErrorActionPreference = "Stop"

if ($Version -notmatch "^[0-9A-Za-z._-]+$") {
    throw "Invalid version: $Version"
}

# Releases from 0.3.3 on are built by GitHub Actions and published as GitHub
# Releases. Older versions remain on the CronPulser download host. A custom
# base URL uses the layout {base}/{version}/{file}.
if ($DownloadBaseUrl) {
    $releaseUrl = "$($DownloadBaseUrl.TrimEnd("/"))/$Version"
} elseif ($Version -in @("0.3.1", "0.3.2")) {
    $releaseUrl = "https://downloads.cronpulser.com/runner/$Version"
} else {
    $releaseUrl = "https://github.com/cronpulser/cronpulser-runner/releases/download/v$Version"
}

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = [Security.Principal.WindowsPrincipal]::new($identity)
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "CronPulser Runner installation must be run from an elevated Administrator PowerShell terminal."
}

if ($env:PROCESSOR_ARCHITECTURE -ne "AMD64" -and $env:PROCESSOR_ARCHITEW6432 -ne "AMD64") {
    throw "This release supports Windows Server on AMD64 only."
}

$temporaryDirectory = $null
try {
    if ($BinaryPath) {
        $runnerBinary = (Resolve-Path -LiteralPath $BinaryPath).Path
    } else {
        $temporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ("cronpulser-" + [Guid]::NewGuid().ToString("N"))
        New-Item -ItemType Directory -Path $temporaryDirectory | Out-Null

        $fileName = "cronpulser-runner-windows-amd64.exe"
        $runnerBinary = Join-Path $temporaryDirectory $fileName
        $binaryUrl = "$releaseUrl/$fileName"
        $checksumsPath = Join-Path $temporaryDirectory "SHA256SUMS"

        Write-Host "Downloading $binaryUrl"
        Invoke-WebRequest -UseBasicParsing -Uri $binaryUrl -OutFile $runnerBinary
        Invoke-WebRequest -UseBasicParsing -Uri "$releaseUrl/SHA256SUMS" -OutFile $checksumsPath

        $checksumLine = Get-Content -LiteralPath $checksumsPath | Where-Object { $_ -match "\s+$([regex]::Escape($fileName))$" } | Select-Object -First 1
        if (-not $checksumLine -or $checksumLine -notmatch "^([0-9a-fA-F]{64})\s+") {
            throw "No valid SHA-256 checksum found for $fileName."
        }
        $expectedHash = $Matches[1].ToUpperInvariant()
        $actualHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $runnerBinary).Hash.ToUpperInvariant()
        if ($actualHash -ne $expectedHash) {
            throw "SHA-256 verification failed for $fileName."
        }
        Write-Host "Verified SHA-256: $actualHash"
    }

    & $runnerBinary install --server $Server --token $Token
    if ($LASTEXITCODE -ne 0) {
        throw "CronPulser Runner installation failed with exit code $LASTEXITCODE."
    }
} finally {
    if ($temporaryDirectory -and (Test-Path -LiteralPath $temporaryDirectory)) {
        Remove-Item -LiteralPath $temporaryDirectory -Recurse -Force
    }
}
