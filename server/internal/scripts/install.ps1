function Install-SentinelAgent {
    param(
        [Parameter(Mandatory = $true)][string]$ServerUrl,
        [Parameter(Mandatory = $true)][string]$EnrollmentToken,
        [switch]$Insecure
    )

    if (-not ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
        Write-Error "This must be run from an elevated (Administrator) PowerShell session."
        return
    }

    $installDir = Join-Path $env:ProgramFiles "SentinelAgent"
    New-Item -ItemType Directory -Force -Path $installDir | Out-Null

    $binUrl = "$ServerUrl/api/v1/downloads/sentinel-agent-windows-amd64.exe"
    $binPath = Join-Path $installDir "sentinel-agent.exe"
    Write-Host "Downloading agent from $binUrl"
    if ($Insecure) {
        Write-Warning "-Insecure set, TLS certificate verification is disabled for this install"
        Invoke-WebRequest -Uri $binUrl -OutFile $binPath -SkipCertificateCheck
    } else {
        Invoke-WebRequest -Uri $binUrl -OutFile $binPath
    }

    $enrollArgs = @("enroll", "--server", $ServerUrl, "--token", $EnrollmentToken)
    if ($Insecure) { $enrollArgs += "--insecure-skip-tls-verify" }
    & $binPath @enrollArgs
    & $binPath install
    & $binPath start

    Write-Host "Sentinel agent installed and started."
}
