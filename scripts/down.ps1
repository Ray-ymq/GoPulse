[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$script:RepoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$script:BackendDirectory = Join-Path $script:RepoRoot 'backend'
$script:FrontendDirectory = Join-Path $script:RepoRoot 'frontend'
$script:ComposeFile = Join-Path $script:RepoRoot 'deploy/compose.yaml'
$script:EnvFile = Join-Path $script:RepoRoot '.env'
$script:EnvExampleFile = Join-Path $script:RepoRoot '.env.example'
$script:RunDirectory = Join-Path $script:RepoRoot '.run'
$script:LockPath = Join-Path $script:RunDirectory 'dev.lock'
$script:BackendRecordPath = Join-Path $script:RunDirectory 'backend.json'
$script:FrontendRecordPath = Join-Path $script:RunDirectory 'frontend.json'
$script:BackendBinary = Join-Path $script:RunDirectory 'bin/gopulse-backend.exe'
$script:ViteConfig = Join-Path $script:FrontendDirectory 'vite.config.ts'
$script:ProjectName = 'gopulse'

function Write-Info {
  param([Parameter(Mandatory)][string]$Message)
  Write-Host "[gopulse] $Message"
}

function Test-PathEqual {
  param([Parameter(Mandatory)][string]$Left, [Parameter(Mandatory)][string]$Right)
  try {
    $leftPath = [System.IO.Path]::GetFullPath($Left).TrimEnd([System.IO.Path]::DirectorySeparatorChar)
    $rightPath = [System.IO.Path]::GetFullPath($Right).TrimEnd([System.IO.Path]::DirectorySeparatorChar)
    return [string]::Equals($leftPath, $rightPath, [System.StringComparison]::OrdinalIgnoreCase)
  } catch { return $false }
}

function Get-ProcessCommandLine {
  param([Parameter(Mandatory)][int]$ProcessId)
  try { return [string](Get-CimInstance Win32_Process -Filter "ProcessId = $ProcessId" -ErrorAction Stop).CommandLine }
  catch { return '' }
}

function Test-ProcessRecord {
  param(
    [Parameter(Mandatory)][string]$Path,
    [Parameter(Mandatory)][string]$ExpectedWorkingDirectory,
    [Parameter(Mandatory)][string]$ExpectedMarker,
    [string]$ExpectedExecutablePath
  )
  if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { return [pscustomobject]@{ Valid = $false; Reason = 'record is absent'; Process = $null } }
  try {
    $record = Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json
    $processId = [int]$record.pid
    $recordStartTicks = [long]$record.startTimeTicks
    $recordExecutable = [string]$record.executablePath
    $recordWorkingDirectory = [string]$record.workingDirectory
    $recordMarker = [string]$record.commandLineMarker
  } catch { return [pscustomobject]@{ Valid = $false; Reason = 'record is malformed'; Process = $null } }

  if (-not (Test-PathEqual -Left $recordWorkingDirectory -Right $ExpectedWorkingDirectory) -or $recordMarker -cne $ExpectedMarker) { return [pscustomobject]@{ Valid = $false; Reason = 'record identity does not match this repository'; Process = $null } }
  if (-not [string]::IsNullOrWhiteSpace($ExpectedExecutablePath) -and -not (Test-PathEqual -Left $recordExecutable -Right $ExpectedExecutablePath)) { return [pscustomobject]@{ Valid = $false; Reason = 'recorded executable does not match the expected application'; Process = $null } }

  try {
    $process = Get-Process -Id $processId -ErrorAction Stop
    $actualStart = $process.StartTime.ToUniversalTime()
    $actualExecutable = $process.Path
  } catch { return [pscustomobject]@{ Valid = $false; Reason = 'recorded process is not running'; Process = $null } }

  if ($actualStart.Ticks -ne $recordStartTicks) { return [pscustomobject]@{ Valid = $false; Reason = 'process start time does not match'; Process = $process } }
  if ([string]::IsNullOrWhiteSpace($actualExecutable) -or -not (Test-PathEqual -Left $actualExecutable -Right $recordExecutable)) { return [pscustomobject]@{ Valid = $false; Reason = 'process executable does not match'; Process = $process } }
  $commandLine = Get-ProcessCommandLine -ProcessId $processId
  if ([string]::IsNullOrWhiteSpace($commandLine) -or $commandLine.IndexOf($ExpectedMarker, [System.StringComparison]::OrdinalIgnoreCase) -lt 0) { return [pscustomobject]@{ Valid = $false; Reason = 'process command line does not match the recorded project context'; Process = $process } }
  return [pscustomobject]@{ Valid = $true; Reason = ''; Process = $process }
}

function Stop-ProcessTree {
  param([Parameter(Mandatory)][int]$ProcessId)
  & taskkill.exe /PID ([string]$ProcessId) /T /F *> $null
  if ($LASTEXITCODE -ne 0) {
    try { Stop-Process -Id $ProcessId -Force -ErrorAction Stop }
    catch [Microsoft.PowerShell.Commands.ProcessCommandException] {}
  }
}

function Stop-RecordedApplication {
  param(
    [Parameter(Mandatory)][string]$Name,
    [Parameter(Mandatory)][string]$Path,
    [Parameter(Mandatory)][string]$ExpectedWorkingDirectory,
    [Parameter(Mandatory)][string]$ExpectedMarker,
    [string]$ExpectedExecutablePath
  )
  if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { Write-Info "$Name is not recorded as running."; return }
  $result = Test-ProcessRecord -Path $Path -ExpectedWorkingDirectory $ExpectedWorkingDirectory -ExpectedMarker $ExpectedMarker -ExpectedExecutablePath $ExpectedExecutablePath
  if ($result.Valid) {
    Stop-ProcessTree -ProcessId $result.Process.Id
    Write-Info "Stopped $Name (PID $($result.Process.Id))."
  } else {
    Write-Info "Removed stale $Name record without stopping a process ($($result.Reason))."
  }
  Remove-Item -LiteralPath $Path -Force -ErrorAction SilentlyContinue
}

function Clear-RunLock {
  if (-not (Test-Path -LiteralPath $script:LockPath -PathType Leaf)) { return }
  for ($attempt = 0; $attempt -lt 50; $attempt++) {
    try {
      $stream = [System.IO.File]::Open($script:LockPath, [System.IO.FileMode]::Open, [System.IO.FileAccess]::ReadWrite, [System.IO.FileShare]::None)
      $stream.Dispose()
      Remove-Item -LiteralPath $script:LockPath -Force
      Write-Info 'Removed the development run lock.'
      return
    } catch [System.IO.IOException] {
      Start-Sleep -Milliseconds 100
    }
  }
  throw [System.InvalidOperationException]::new('The development run lock is still active. Stop the foreground dev script with Ctrl+C, then retry.')
}

function Read-DotEnv {
  param([Parameter(Mandatory)][string]$Path)
  $values = @{}
  if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { return $values }
  foreach ($rawLine in [System.IO.File]::ReadAllLines($Path)) {
    $line = $rawLine.Trim()
    if ($line.Length -eq 0 -or $line.StartsWith('#')) { continue }
    if ($line -match '^([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)$') {
      $value = $Matches[2].Trim()
      if ($value.Length -ge 2 -and (($value[0] -eq "'" -and $value[$value.Length - 1] -eq "'") -or ($value[0] -eq '"' -and $value[$value.Length - 1] -eq '"'))) { $value = $value.Substring(1, $value.Length - 2) }
      $values[$Matches[1]] = $value
    }
  }
  return $values
}

function Invoke-ComposeDown {
  $docker = (Get-Command 'docker' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1).Source
  if ([string]::IsNullOrWhiteSpace($docker)) { throw [System.InvalidOperationException]::new('Docker is required to stop Compose infrastructure.') }
  & $docker compose version *> $null
  if ($LASTEXITCODE -ne 0) { throw [System.InvalidOperationException]::new('Docker Compose is unavailable.') }

  $envPath = if (Test-Path -LiteralPath $script:EnvFile -PathType Leaf) { $script:EnvFile } else { $script:EnvExampleFile }
  if (-not (Test-Path -LiteralPath $envPath -PathType Leaf)) { throw [System.InvalidOperationException]::new('Neither .env nor .env.example is available for Compose interpolation.') }
  $dotenv = Read-DotEnv -Path $envPath
  $keys = @('MYSQL_DATABASE', 'MYSQL_USER', 'MYSQL_PASSWORD', 'MYSQL_ROOT_PASSWORD', 'MYSQL_PORT', 'REDIS_PASSWORD', 'REDIS_PORT', 'RABBITMQ_USER', 'RABBITMQ_PASSWORD', 'RABBITMQ_PORT', 'RABBITMQ_MANAGEMENT_PORT')
  $previous = @{}
  $current = [System.Environment]::GetEnvironmentVariables('Process')
  foreach ($key in $keys) {
    $previous[$key] = [pscustomobject]@{ Exists = $current.Contains($key); Value = [System.Environment]::GetEnvironmentVariable($key, 'Process') }
    if (-not $previous[$key].Exists -and $dotenv.ContainsKey($key)) { [System.Environment]::SetEnvironmentVariable($key, [string]$dotenv[$key], 'Process') }
  }
  try {
    & $docker compose --project-name $script:ProjectName --env-file $envPath --file $script:ComposeFile down
    if ($LASTEXITCODE -ne 0) { throw [System.InvalidOperationException]::new("Docker Compose down failed with exit code $LASTEXITCODE.") }
  } finally {
    foreach ($key in $keys) {
      if ($previous[$key].Exists) { [System.Environment]::SetEnvironmentVariable($key, [string]$previous[$key].Value, 'Process') }
      else { Remove-Item -LiteralPath "Env:$key" -ErrorAction SilentlyContinue }
    }
  }
  Write-Info 'Compose infrastructure is stopped; named volumes were preserved.'
}

try {
  $nodeCommand = (Get-Command 'node' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1).Source
  Stop-RecordedApplication -Name 'Frontend' -Path $script:FrontendRecordPath -ExpectedWorkingDirectory $script:FrontendDirectory -ExpectedMarker $script:ViteConfig -ExpectedExecutablePath $nodeCommand
  Stop-RecordedApplication -Name 'Backend' -Path $script:BackendRecordPath -ExpectedWorkingDirectory $script:BackendDirectory -ExpectedMarker $script:BackendBinary -ExpectedExecutablePath $script:BackendBinary
  Clear-RunLock
  Invoke-ComposeDown
  exit 0
} catch {
  [Console]::Error.WriteLine("[gopulse] ERROR: $($_.Exception.Message)")
  exit 1
}
