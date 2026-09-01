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
$script:BinDirectory = Join-Path $script:RunDirectory 'bin'
$script:LockPath = Join-Path $script:RunDirectory 'dev.lock'
$script:BackendRecordPath = Join-Path $script:RunDirectory 'backend.json'
$script:FrontendRecordPath = Join-Path $script:RunDirectory 'frontend.json'
$script:BackendBinary = Join-Path $script:BinDirectory 'gopulse-backend.exe'
$script:ViteCli = Join-Path $script:FrontendDirectory 'node_modules/vite/bin/vite.js'
$script:ViteConfig = Join-Path $script:FrontendDirectory 'vite.config.ts'
$script:ProjectName = 'gopulse'
$script:LockOwned = $false
$script:LockStream = $null
$script:LockToken = $null
$script:BackendProcess = $null
$script:FrontendProcess = $null
$script:BackendStarted = $false
$script:FrontendStarted = $false
$script:ComposeEnvironment = @{}
$script:CallerEnvironment = @{}

foreach ($entry in [System.Environment]::GetEnvironmentVariables('Process').GetEnumerator()) {
  $script:CallerEnvironment[[string]$entry.Key] = [string]$entry.Value
}

$script:ComposeKeys = @(
  'PUBLISHED_HOST', 'MYSQL_DATABASE', 'MYSQL_USER', 'MYSQL_PASSWORD', 'MYSQL_ROOT_PASSWORD', 'MYSQL_PORT',
  'REDIS_PASSWORD', 'REDIS_PORT', 'RABBITMQ_USER', 'RABBITMQ_PASSWORD',
  'RABBITMQ_PORT', 'RABBITMQ_MANAGEMENT_PORT'
)
$script:BackendKeys = @(
  'APP_ENV', 'HTTP_HOST', 'HTTP_PORT', 'MYSQL_HOST', 'MYSQL_PORT', 'MYSQL_DATABASE',
  'MYSQL_USER', 'MYSQL_PASSWORD', 'REDIS_HOST', 'REDIS_PORT', 'REDIS_PASSWORD',
  'REDIS_DB', 'RABBITMQ_URL', 'AUTH_JWT_SECRET', 'AUTH_JWT_TTL', 'AUTH_COOKIE_NAME',
  'AUTH_COOKIE_SECURE', 'REDIS_POST_DETAIL_TTL', 'REDIS_OPERATION_TIMEOUT'
)
$script:AllConfigKeys = @($script:ComposeKeys + $script:BackendKeys | Sort-Object -Unique)
$script:Defaults = @{
  APP_ENV = 'development'; PUBLISHED_HOST = '127.0.0.1'; HTTP_HOST = '127.0.0.1'; HTTP_PORT = '8080'
  MYSQL_HOST = '127.0.0.1'; MYSQL_PORT = '3306'
  REDIS_HOST = '127.0.0.1'; REDIS_PORT = '6379'; REDIS_DB = '0'
  RABBITMQ_PORT = '5672'; RABBITMQ_MANAGEMENT_PORT = '15672'
  AUTH_JWT_TTL = '2h'; AUTH_COOKIE_NAME = 'gopulse_session'; AUTH_COOKIE_SECURE = 'false'
  REDIS_POST_DETAIL_TTL = '5m'; REDIS_OPERATION_TIMEOUT = '200ms'
}
$script:RequiredKeys = @(
  'MYSQL_DATABASE', 'MYSQL_USER', 'MYSQL_PASSWORD', 'MYSQL_ROOT_PASSWORD',
  'REDIS_PASSWORD', 'RABBITMQ_USER', 'RABBITMQ_PASSWORD', 'RABBITMQ_URL',
  'AUTH_JWT_SECRET'
)

function Write-Info {
  param([Parameter(Mandatory)][string]$Message)
  Write-Host "[gopulse] $Message"
}

function Throw-DevError {
  param([Parameter(Mandatory)][string]$Message)
  throw [System.InvalidOperationException]::new($Message)
}

function Test-PathEqual {
  param([Parameter(Mandatory)][string]$Left, [Parameter(Mandatory)][string]$Right)
  try {
    $leftPath = [System.IO.Path]::GetFullPath($Left).TrimEnd([System.IO.Path]::DirectorySeparatorChar)
    $rightPath = [System.IO.Path]::GetFullPath($Right).TrimEnd([System.IO.Path]::DirectorySeparatorChar)
    return [string]::Equals($leftPath, $rightPath, [System.StringComparison]::OrdinalIgnoreCase)
  } catch {
    return $false
  }
}

function Test-RequiredTools {
  $script:GoCommand = (Get-Command 'go' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1).Source
  $script:NodeCommand = (Get-Command 'node' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1).Source
  $npmCommand = Get-Command 'npm.cmd' -CommandType Application -ErrorAction SilentlyContinue
  if ($null -eq $npmCommand) { $npmCommand = Get-Command 'npm' -ErrorAction SilentlyContinue }
  $script:NpmCommand = if ($null -ne $npmCommand) { $npmCommand.Source } else { $null }
  $script:DockerCommand = (Get-Command 'docker' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1).Source

  $missing = @()
  if ([string]::IsNullOrWhiteSpace($script:GoCommand)) { $missing += 'Go' }
  if ([string]::IsNullOrWhiteSpace($script:NodeCommand)) { $missing += 'Node.js' }
  if ([string]::IsNullOrWhiteSpace($script:NpmCommand)) { $missing += 'npm' }
  if ([string]::IsNullOrWhiteSpace($script:DockerCommand)) { $missing += 'Docker' }
  if ($missing.Count -gt 0) { Throw-DevError "Missing required tool(s): $($missing -join ', ')." }

  & $script:DockerCommand compose version *> $null
  if ($LASTEXITCODE -ne 0) { Throw-DevError 'Docker Compose is unavailable. Install a Docker CLI with the Compose plugin.' }
  & $script:DockerCommand info *> $null
  if ($LASTEXITCODE -ne 0) { Throw-DevError 'Docker is installed, but the Docker daemon is unavailable.' }
  Write-Info 'Required tools are available.'
}

function Read-DotEnv {
  param([Parameter(Mandatory)][string]$Path)
  if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { Throw-DevError "Environment file not found: $Path" }

  $values = @{}
  $lineNumber = 0
  foreach ($rawLine in [System.IO.File]::ReadAllLines($Path)) {
    $lineNumber++
    $line = $rawLine.Trim()
    if ($line.Length -eq 0 -or $line.StartsWith('#')) { continue }
    if ($line -match '^export(?:\s|$)') { Throw-DevError "Unsupported dotenv syntax at line ${lineNumber}: export is not allowed." }
    if ($line -notmatch '^([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)$') { Throw-DevError "Invalid dotenv assignment at line $lineNumber." }

    $key = $Matches[1]
    $value = $Matches[2].Trim()
    if ($value.Length -gt 0 -and ($value[0] -eq "'" -or $value[0] -eq '"')) {
      $quote = $value[0]
      if ($value.Length -lt 2 -or $value[$value.Length - 1] -ne $quote) { Throw-DevError "Unterminated quoted dotenv value for $key at line $lineNumber." }
      $value = $value.Substring(1, $value.Length - 2)
      if ($value.Contains([string]$quote)) { Throw-DevError "Embedded quote syntax is not supported for $key at line $lineNumber." }
    } elseif ($value.EndsWith("'") -or $value.EndsWith('"')) {
      Throw-DevError "Mismatched quote in dotenv value for $key at line $lineNumber."
    }
    if ($value.Contains("`r") -or $value.Contains("`n") -or $value.Contains([char]0)) { Throw-DevError "Multiline or null dotenv values are not supported for $key at line $lineNumber." }
    $values[$key] = $value
  }
  return $values
}

function Resolve-Configuration {
  param([Parameter(Mandatory)][hashtable]$DotEnv)
  $resolved = @{}
  foreach ($key in $script:AllConfigKeys) {
    if ($script:CallerEnvironment.ContainsKey($key)) { $resolved[$key] = [string]$script:CallerEnvironment[$key] }
    elseif ($DotEnv.ContainsKey($key)) { $resolved[$key] = [string]$DotEnv[$key] }
    elseif ($script:Defaults.ContainsKey($key)) { $resolved[$key] = [string]$script:Defaults[$key] }
  }
  foreach ($key in $script:RequiredKeys) {
    if (-not $resolved.ContainsKey($key) -or [string]::IsNullOrWhiteSpace([string]$resolved[$key])) { Throw-DevError "Required configuration $key is missing." }
  }
  foreach ($key in @('HTTP_PORT', 'MYSQL_PORT', 'REDIS_PORT', 'RABBITMQ_PORT', 'RABBITMQ_MANAGEMENT_PORT')) {
    $port = 0
    if (-not $resolved.ContainsKey($key) -or -not [int]::TryParse([string]$resolved[$key], [ref]$port) -or $port -lt 1 -or $port -gt 65535) { Throw-DevError "$key must be an integer between 1 and 65535." }
    $resolved[$key] = [string]$port
  }
  $redisDatabase = 0
  if (-not $resolved.ContainsKey('REDIS_DB') -or -not [int]::TryParse([string]$resolved['REDIS_DB'], [ref]$redisDatabase) -or $redisDatabase -lt 0) { Throw-DevError 'REDIS_DB must be a non-negative integer.' }
  $resolved['REDIS_DB'] = [string]$redisDatabase

  $uri = $null
  if (-not [System.Uri]::TryCreate([string]$resolved['RABBITMQ_URL'], [System.UriKind]::Absolute, [ref]$uri) -or $uri.Scheme -notin @('amqp', 'amqps')) { Throw-DevError 'RABBITMQ_URL must be a valid amqp or amqps URL.' }
  $credentials = $uri.UserInfo.Split(':', 2)
  if ($credentials.Count -ne 2) { Throw-DevError 'RABBITMQ_URL must include URL-encoded username and password credentials.' }
  try {
    $urlUser = [System.Uri]::UnescapeDataString($credentials[0])
    $urlPassword = [System.Uri]::UnescapeDataString($credentials[1])
  } catch {
    Throw-DevError 'RABBITMQ_URL contains invalid URL-encoded credentials.'
  }
  if ($urlUser -cne [string]$resolved['RABBITMQ_USER'] -or $urlPassword -cne [string]$resolved['RABBITMQ_PASSWORD']) { Throw-DevError 'RABBITMQ_URL credentials must match RABBITMQ_USER and RABBITMQ_PASSWORD.' }
  return $resolved
}

function Invoke-WithEnvironment {
  param([Parameter(Mandatory)][hashtable]$Values, [Parameter(Mandatory)][scriptblock]$Action)
  $previous = @{}
  $current = [System.Environment]::GetEnvironmentVariables('Process')
  foreach ($key in $Values.Keys) {
    $previous[$key] = [pscustomobject]@{ Exists = $current.Contains($key); Value = [System.Environment]::GetEnvironmentVariable($key, 'Process') }
    [System.Environment]::SetEnvironmentVariable($key, [string]$Values[$key], 'Process')
  }
  try { return & $Action }
  finally {
    foreach ($key in $Values.Keys) {
      if ($previous[$key].Exists) { [System.Environment]::SetEnvironmentVariable($key, [string]$previous[$key].Value, 'Process') }
      else { Remove-Item -LiteralPath "Env:$key" -ErrorAction SilentlyContinue }
    }
  }
}

function Set-ComposeEnvironment {
  param([Parameter(Mandatory)][hashtable]$Configuration)
  $script:ComposeEnvironment = @{}
  foreach ($key in $script:ComposeKeys) { $script:ComposeEnvironment[$key] = [string]$Configuration[$key] }
}

function Invoke-ComposeVisible {
  param([Parameter(Mandatory)][string[]]$CommandArguments)
  $dockerArguments = @('compose', '--project-name', $script:ProjectName, '--env-file', $script:EnvFile, '--file', $script:ComposeFile) + $CommandArguments
  Invoke-WithEnvironment -Values $script:ComposeEnvironment -Action {
    & $script:DockerCommand @dockerArguments
    if ($LASTEXITCODE -ne 0) { Throw-DevError "Docker Compose command failed with exit code $LASTEXITCODE." }
  }
}

function Get-ContainerDetails {
  param([Parameter(Mandatory)][string]$Service)
  $ids = @(& $script:DockerCommand ps --filter "label=com.docker.compose.project=$($script:ProjectName)" --filter "label=com.docker.compose.service=$Service" --format '{{.ID}}' 2>$null)
  if ($LASTEXITCODE -ne 0 -or $ids.Count -eq 0) { return @() }
  $containers = @()
  foreach ($id in $ids) {
    if ([string]::IsNullOrWhiteSpace([string]$id)) { continue }
    $json = (& $script:DockerCommand inspect ([string]$id) 2>$null | Out-String)
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($json)) { continue }
    try {
      $parsed = ConvertFrom-Json $json
      $containers += if ($parsed -is [array]) { $parsed[0] } else { $parsed }
    } catch {}
  }
  return $containers
}

function Test-HealthyComposePort {
  param([Parameter(Mandatory)][string]$Service, [Parameter(Mandatory)][string]$ContainerPort, [Parameter(Mandatory)][int]$HostPort)
  foreach ($container in Get-ContainerDetails -Service $Service) {
    if (-not [bool]$container.State.Running -or [string]$container.State.Health.Status -ne 'healthy') { continue }
    $portProperty = $container.NetworkSettings.Ports.PSObject.Properties[$ContainerPort]
    if ($null -eq $portProperty -or $null -eq $portProperty.Value) { continue }
    foreach ($binding in @($portProperty.Value)) {
      if ([string]$binding.HostPort -eq [string]$HostPort) { return $true }
    }
  }
  return $false
}

function Get-PortListeners {
  param([Parameter(Mandatory)][int]$Port)
  $listeners = @()
  if ($null -ne (Get-Command 'Get-NetTCPConnection' -ErrorAction SilentlyContinue)) {
    foreach ($connection in @(Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue)) {
      $pidValue = [int]$connection.OwningProcess
      $processName = 'unknown'; $processPath = 'unavailable'
      try {
        $process = Get-Process -Id $pidValue -ErrorAction Stop
        $processName = $process.ProcessName
        if (-not [string]::IsNullOrWhiteSpace($process.Path)) { $processPath = $process.Path }
      } catch {}
      $listeners += "PID $pidValue ($processName, $processPath)"
    }
    return @($listeners | Sort-Object -Unique)
  }
  foreach ($match in @(netstat -ano -p tcp 2>$null | Select-String -Pattern ":$Port\s+.*LISTENING\s+(\d+)\s*$")) { $listeners += $match.Line.Trim() }
  return @($listeners | Sort-Object -Unique)
}

function Test-RequiredPorts {
  param([Parameter(Mandatory)][hashtable]$Configuration)
  $definitions = @(
    [pscustomobject]@{ Name = 'Backend'; Port = [int]$Configuration['HTTP_PORT']; Service = $null; ContainerPort = $null },
    [pscustomobject]@{ Name = 'Frontend'; Port = 5173; Service = $null; ContainerPort = $null },
    [pscustomobject]@{ Name = 'MySQL'; Port = [int]$Configuration['MYSQL_PORT']; Service = 'mysql'; ContainerPort = '3306/tcp' },
    [pscustomobject]@{ Name = 'Redis'; Port = [int]$Configuration['REDIS_PORT']; Service = 'redis'; ContainerPort = '6379/tcp' },
    [pscustomobject]@{ Name = 'RabbitMQ'; Port = [int]$Configuration['RABBITMQ_PORT']; Service = 'rabbitmq'; ContainerPort = '5672/tcp' },
    [pscustomobject]@{ Name = 'RabbitMQ management'; Port = [int]$Configuration['RABBITMQ_MANAGEMENT_PORT']; Service = 'rabbitmq'; ContainerPort = '15672/tcp' }
  )
  foreach ($group in $definitions | Group-Object Port) {
    if ($group.Count -gt 1) { Throw-DevError "Configured services share port $($group.Name): $(@($group.Group | ForEach-Object Name) -join ', ')." }
  }
  foreach ($definition in $definitions) {
    $listeners = @(Get-PortListeners -Port $definition.Port)
    if ($listeners.Count -eq 0) { continue }
    if ($null -ne $definition.Service -and (Test-HealthyComposePort -Service $definition.Service -ContainerPort $definition.ContainerPort -HostPort $definition.Port)) {
      Write-Info "$($definition.Name) already uses port $($definition.Port) through a healthy gopulse container."
      continue
    }
    Throw-DevError "Port $($definition.Port) required by $($definition.Name) is already in use: $($listeners -join '; ')."
  }
  Write-Info 'Required ports are available or owned by healthy gopulse containers.'
}

function Get-ProcessCommandLine {
  param([Parameter(Mandatory)][int]$ProcessId)
  try {
    $instance = Get-CimInstance Win32_Process -Filter "ProcessId = $ProcessId" -ErrorAction Stop
    return [string]$instance.CommandLine
  } catch { return '' }
}

function Test-ProcessRecord {
  param(
    [Parameter(Mandatory)][string]$Path,
    [Parameter(Mandatory)][string]$ExpectedWorkingDirectory,
    [Parameter(Mandatory)][string]$ExpectedMarker,
    [string]$ExpectedExecutablePath
  )
  if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { return [pscustomobject]@{ Valid = $false; Reason = 'record is absent'; Process = $null; Record = $null } }
  try {
    $record = Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json
    $processId = [int]$record.pid
    $recordStartTicks = [long]$record.startTimeTicks
    $recordExecutable = [string]$record.executablePath
    $recordWorkingDirectory = [string]$record.workingDirectory
    $recordMarker = [string]$record.commandLineMarker
  } catch {
    return [pscustomobject]@{ Valid = $false; Reason = 'record is malformed'; Process = $null; Record = $null }
  }
  if (-not (Test-PathEqual -Left $recordWorkingDirectory -Right $ExpectedWorkingDirectory) -or $recordMarker -cne $ExpectedMarker) { return [pscustomobject]@{ Valid = $false; Reason = 'record identity does not match this repository'; Process = $null; Record = $record } }
  if (-not [string]::IsNullOrWhiteSpace($ExpectedExecutablePath) -and -not (Test-PathEqual -Left $recordExecutable -Right $ExpectedExecutablePath)) { return [pscustomobject]@{ Valid = $false; Reason = 'recorded executable does not match the expected application'; Process = $null; Record = $record } }
  try {
    $process = Get-Process -Id $processId -ErrorAction Stop
    $actualStart = $process.StartTime.ToUniversalTime()
    $actualExecutable = $process.Path
  } catch {
    return [pscustomobject]@{ Valid = $false; Reason = 'recorded process is not running'; Process = $null; Record = $record }
  }
  if ($actualStart.Ticks -ne $recordStartTicks) { return [pscustomobject]@{ Valid = $false; Reason = 'process start time does not match'; Process = $process; Record = $record } }
  if ([string]::IsNullOrWhiteSpace($actualExecutable) -or -not (Test-PathEqual -Left $actualExecutable -Right $recordExecutable)) { return [pscustomobject]@{ Valid = $false; Reason = 'process executable does not match'; Process = $process; Record = $record } }
  $commandLine = Get-ProcessCommandLine -ProcessId $processId
  if ([string]::IsNullOrWhiteSpace($commandLine) -or $commandLine.IndexOf($ExpectedMarker, [System.StringComparison]::OrdinalIgnoreCase) -lt 0) { return [pscustomobject]@{ Valid = $false; Reason = 'process command line does not match the recorded project working context'; Process = $process; Record = $record } }
  return [pscustomobject]@{ Valid = $true; Reason = ''; Process = $process; Record = $record }
}

function Remove-Or-RejectExistingRecord {
  param(
    [Parameter(Mandatory)][string]$Name,
    [Parameter(Mandatory)][string]$Path,
    [Parameter(Mandatory)][string]$ExpectedWorkingDirectory,
    [Parameter(Mandatory)][string]$ExpectedMarker,
    [string]$ExpectedExecutablePath
  )
  if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { return }
  $result = Test-ProcessRecord -Path $Path -ExpectedWorkingDirectory $ExpectedWorkingDirectory -ExpectedMarker $ExpectedMarker -ExpectedExecutablePath $ExpectedExecutablePath
  if ($result.Valid) { Throw-DevError "$Name is already running under this repository. Run scripts/down.ps1 before starting another dev session." }
  Remove-Item -LiteralPath $Path -Force -ErrorAction SilentlyContinue
  Write-Info "Removed stale $Name process record ($($result.Reason))."
}

function Acquire-RunLock {
  New-Item -ItemType Directory -Path $script:RunDirectory -Force | Out-Null
  New-Item -ItemType Directory -Path $script:BinDirectory -Force | Out-Null
  for ($attempt = 0; $attempt -lt 2; $attempt++) {
    try {
      $stream = [System.IO.File]::Open($script:LockPath, [System.IO.FileMode]::CreateNew, [System.IO.FileAccess]::ReadWrite, [System.IO.FileShare]::None)
      $script:LockToken = [System.Guid]::NewGuid().ToString('N')
      $current = Get-Process -Id $PID
      $record = [ordered]@{
        pid = $PID
        startTime = $current.StartTime.ToUniversalTime().ToString('O')
        executablePath = $current.Path
        workingDirectory = $script:RepoRoot
        scriptPath = $PSCommandPath
        platform = 'windows'
        token = $script:LockToken
      }
      $writer = [System.IO.StreamWriter]::new($stream, [System.Text.UTF8Encoding]::new($false), 1024, $true)
      try {
        $writer.Write(($record | ConvertTo-Json -Compress))
        $writer.Flush()
        $stream.Flush($true)
      } finally { $writer.Dispose() }
      $script:LockStream = $stream
      $script:LockOwned = $true
      Write-Info 'Acquired the development run lock.'
      return
    } catch [System.IO.IOException] {
      if (-not (Test-Path -LiteralPath $script:LockPath)) { continue }
      try {
        $probe = [System.IO.File]::Open($script:LockPath, [System.IO.FileMode]::Open, [System.IO.FileAccess]::ReadWrite, [System.IO.FileShare]::None)
        try {
          $reader = [System.IO.StreamReader]::new($probe, [System.Text.UTF8Encoding]::new($false), $true, 1024, $true)
          try { $existingText = $reader.ReadToEnd() } finally { $reader.Dispose() }
        } finally { $probe.Dispose() }
        $foreignPlatform = $false
        try { $foreignPlatform = [string](($existingText | ConvertFrom-Json).platform) -ne 'windows' } catch {}
        if ($foreignPlatform) { Throw-DevError 'A development lock from another platform exists. Run the matching down script before retrying.' }
        Remove-Item -LiteralPath $script:LockPath -Force
        Write-Info 'Removed an unlocked stale development lock.'
      } catch [System.IO.IOException] {
        Throw-DevError 'Another development session is already running for this repository.'
      }
    }
  }
  Throw-DevError 'Unable to acquire the development run lock.'
}

function Release-RunLock {
  if (-not $script:LockOwned) { return }
  if ($null -ne $script:LockStream) { $script:LockStream.Dispose(); $script:LockStream = $null }
  try {
    if (Test-Path -LiteralPath $script:LockPath -PathType Leaf) {
      $record = Get-Content -LiteralPath $script:LockPath -Raw | ConvertFrom-Json
      if ([string]$record.token -eq $script:LockToken) { Remove-Item -LiteralPath $script:LockPath -Force }
    }
  } catch { Write-Warning "Unable to remove development lock: $($_.Exception.Message)" }
  finally { $script:LockOwned = $false }
}

function Get-ComposeServiceHealth {
  param([Parameter(Mandatory)][string]$Service)
  $containers = @(Get-ContainerDetails -Service $Service)
  if ($containers.Count -eq 0) { return 'missing' }
  $container = $containers[0]
  if (-not [bool]$container.State.Running) { return "stopped:$($container.State.Status)" }
  if ($null -eq $container.State.Health) { return 'no-healthcheck' }
  return [string]$container.State.Health.Status
}

function Wait-ForInfrastructure {
  $services = @('mysql', 'redis', 'rabbitmq')
  $deadline = [System.DateTimeOffset]::UtcNow.AddSeconds(180)
  Write-Info 'Waiting for MySQL, Redis, and RabbitMQ healthchecks.'
  while ([System.DateTimeOffset]::UtcNow -lt $deadline) {
    $statuses = @{}
    foreach ($service in $services) { $statuses[$service] = Get-ComposeServiceHealth -Service $service }
    if (@($services | Where-Object { $statuses[$_] -ne 'healthy' }).Count -eq 0) { Write-Info 'Infrastructure is healthy.'; return }
    if (@($services | Where-Object { $statuses[$_] -like 'stopped:*' }).Count -gt 0) { break }
    Start-Sleep -Seconds 2
  }
  $failed = @()
  foreach ($service in $services) {
    $status = Get-ComposeServiceHealth -Service $service
    if ($status -ne 'healthy') { $failed += "$service=$status" }
  }
  $diagnostic = "docker compose --project-name $($script:ProjectName) --env-file `"$($script:EnvFile)`" --file `"$($script:ComposeFile)`" ps"
  Throw-DevError "Infrastructure did not become healthy ($($failed -join ', ')). Inspect it with: $diagnostic"
}

function Invoke-DatabaseMigrations {
  param([Parameter(Mandatory)][hashtable]$Configuration)
  Write-Info 'Applying database migrations.'
  $backendEnvironment = @{}
  foreach ($key in $script:BackendKeys) {
    if ($Configuration.ContainsKey($key)) { $backendEnvironment[$key] = [string]$Configuration[$key] }
  }
  Invoke-WithEnvironment -Values $backendEnvironment -Action {
    Push-Location $script:BackendDirectory
    try {
      & $script:GoCommand run ./cmd/migrate up
      if ($LASTEXITCODE -ne 0) { Throw-DevError "Database migration failed with exit code $LASTEXITCODE." }
    } finally { Pop-Location }
  }
}

function Ensure-FrontendDependencies {
  $packageLock = Join-Path $script:FrontendDirectory 'package-lock.json'
  $nodeModules = Join-Path $script:FrontendDirectory 'node_modules'
  $hashMarker = Join-Path $nodeModules '.gopulse-package-lock.sha256'
  if (-not (Test-Path -LiteralPath $packageLock -PathType Leaf)) { Throw-DevError 'frontend/package-lock.json is required for reproducible installation.' }
  $lockHash = (Get-FileHash -LiteralPath $packageLock -Algorithm SHA256).Hash.ToLowerInvariant()
  $nodePlatform = (& $script:NodeCommand -p 'process.platform + "-" + process.arch').Trim()
  if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($nodePlatform)) { Throw-DevError 'Unable to determine the Node.js platform for dependency validation.' }
  $installFingerprint = "$lockHash|$nodePlatform"
  $needsInstall = -not (Test-Path -LiteralPath $nodeModules -PathType Container)
  if (-not $needsInstall) {
    $recordedHash = if (Test-Path -LiteralPath $hashMarker -PathType Leaf) { (Get-Content -LiteralPath $hashMarker -Raw).Trim() } else { '' }
    $needsInstall = $recordedHash -cne $installFingerprint
  }
  if (-not $needsInstall) {
    Push-Location $script:FrontendDirectory
    try { & $script:NpmCommand ls --depth=0 --ignore-scripts *> $null; $needsInstall = $LASTEXITCODE -ne 0 }
    finally { Pop-Location }
  }
  if ($needsInstall) {
    Write-Info 'Frontend dependencies are missing or do not match package-lock.json; running npm ci.'
    Push-Location $script:FrontendDirectory
    try {
      & $script:NpmCommand ci
      if ($LASTEXITCODE -ne 0) { Throw-DevError "npm ci failed with exit code $LASTEXITCODE." }
    } finally { Pop-Location }
    [System.IO.File]::WriteAllText($hashMarker, "$installFingerprint`n", [System.Text.UTF8Encoding]::new($false))
  } else { Write-Info 'Frontend dependencies match package-lock.json.' }
}

function Build-Backend {
  Write-Info 'Building the Backend development executable.'
  Push-Location $script:BackendDirectory
  try {
    & $script:GoCommand build -o $script:BackendBinary ./cmd/server
    if ($LASTEXITCODE -ne 0) { Throw-DevError "Backend build failed with exit code $LASTEXITCODE." }
  } finally { Pop-Location }
}

function New-ChildEnvironment {
  param([Parameter(Mandatory)][hashtable]$Configuration, [Parameter(Mandatory)][ValidateSet('backend', 'frontend')][string]$Kind)
  $environment = @{}
  foreach ($key in $script:CallerEnvironment.Keys) {
    if ($Kind -eq 'frontend' -and $script:AllConfigKeys -contains [string]$key) { continue }
    $environment[[string]$key] = [string]$script:CallerEnvironment[$key]
  }
  if ($Kind -eq 'backend') {
    foreach ($key in $script:BackendKeys) { $environment[$key] = [string]$Configuration[$key] }
  }
  return $environment
}

function Start-DirectProcess {
  param(
    [Parameter(Mandatory)][string]$FilePath,
    [Parameter(Mandatory)][AllowEmptyCollection()][string[]]$Arguments,
    [Parameter(Mandatory)][string]$WorkingDirectory,
    [Parameter(Mandatory)][hashtable]$Environment
  )
  $startInfo = [System.Diagnostics.ProcessStartInfo]::new()
  $startInfo.FileName = $FilePath
  $startInfo.WorkingDirectory = $WorkingDirectory
  $startInfo.UseShellExecute = $false
  foreach ($argument in $Arguments) { [void]$startInfo.ArgumentList.Add($argument) }
  $startInfo.Environment.Clear()
  foreach ($key in $Environment.Keys) { $startInfo.Environment[[string]$key] = [string]$Environment[$key] }
  $process = [System.Diagnostics.Process]::Start($startInfo)
  if ($null -eq $process) { Throw-DevError "Failed to start process: $FilePath" }
  Start-Sleep -Milliseconds 600
  $process.Refresh()
  if ($process.HasExited) { Throw-DevError "Process exited during startup with code $($process.ExitCode): $FilePath" }
  return $process
}

function Write-ProcessRecord {
  param(
    [Parameter(Mandatory)][System.Diagnostics.Process]$Process,
    [Parameter(Mandatory)][string]$Path,
    [Parameter(Mandatory)][string]$WorkingDirectory,
    [Parameter(Mandatory)][string]$CommandLineMarker
  )
  $Process.Refresh()
  $record = [ordered]@{
    pid = $Process.Id
    startTime = $Process.StartTime.ToUniversalTime().ToString('O')
    startTimeTicks = $Process.StartTime.ToUniversalTime().Ticks
    executablePath = $Process.Path
    workingDirectory = [System.IO.Path]::GetFullPath($WorkingDirectory)
    commandLineMarker = $CommandLineMarker
  }
  $temporaryPath = "$Path.$([System.Guid]::NewGuid().ToString('N')).tmp"
  try {
    [System.IO.File]::WriteAllText($temporaryPath, ($record | ConvertTo-Json -Compress), [System.Text.UTF8Encoding]::new($false))
    [System.IO.File]::Move($temporaryPath, $Path, $true)
  } finally { Remove-Item -LiteralPath $temporaryPath -Force -ErrorAction SilentlyContinue }
}

function Stop-ProcessTree {
  param([Parameter(Mandatory)][int]$ProcessId)
  & taskkill.exe /PID ([string]$ProcessId) /T /F *> $null
  if ($LASTEXITCODE -ne 0) {
    try {
      $process = Get-Process -Id $ProcessId -ErrorAction Stop
      Stop-Process -InputObject $process -Force -ErrorAction Stop
    } catch [Microsoft.PowerShell.Commands.ProcessCommandException] {}
  }
}

function Stop-RecordedApplication {
  param(
    [Parameter(Mandatory)][string]$Name,
    [Parameter(Mandatory)][string]$Path,
    [Parameter(Mandatory)][string]$ExpectedWorkingDirectory,
    [Parameter(Mandatory)][string]$ExpectedMarker,
    [string]$ExpectedExecutablePath,
    [System.Diagnostics.Process]$FallbackProcess
  )
  if (Test-Path -LiteralPath $Path -PathType Leaf) {
    $result = Test-ProcessRecord -Path $Path -ExpectedWorkingDirectory $ExpectedWorkingDirectory -ExpectedMarker $ExpectedMarker -ExpectedExecutablePath $ExpectedExecutablePath
    if ($result.Valid) {
      Stop-ProcessTree -ProcessId $result.Process.Id
      Write-Info "Stopped $Name (PID $($result.Process.Id))."
    } else {
      Write-Info "Removed stale $Name record without stopping a process ($($result.Reason))."
    }
    Remove-Item -LiteralPath $Path -Force -ErrorAction SilentlyContinue
    return
  }
  if ($null -ne $FallbackProcess) {
    try {
      $FallbackProcess.Refresh()
      if (-not $FallbackProcess.HasExited) {
        Stop-ProcessTree -ProcessId $FallbackProcess.Id
        Write-Info "Stopped $Name (PID $($FallbackProcess.Id))."
      }
    } catch {}
  }
}

function Start-Applications {
  param([Parameter(Mandatory)][hashtable]$Configuration)
  $backendEnvironment = New-ChildEnvironment -Configuration $Configuration -Kind backend
  $frontendEnvironment = New-ChildEnvironment -Configuration $Configuration -Kind frontend

  Write-Info 'Starting Backend.'
  $script:BackendProcess = Start-DirectProcess -FilePath $script:BackendBinary -Arguments @() -WorkingDirectory $script:BackendDirectory -Environment $backendEnvironment
  $script:BackendStarted = $true
  Write-ProcessRecord -Process $script:BackendProcess -Path $script:BackendRecordPath -WorkingDirectory $script:BackendDirectory -CommandLineMarker $script:BackendBinary

  if (-not (Test-Path -LiteralPath $script:ViteCli -PathType Leaf)) { Throw-DevError 'The project-local Vite CLI is missing after dependency installation.' }
  Write-Info 'Starting Frontend.'
  $frontendArguments = @($script:ViteCli, '--host', 'localhost', '--strictPort', '--config', $script:ViteConfig)
  $script:FrontendProcess = Start-DirectProcess -FilePath $script:NodeCommand -Arguments $frontendArguments -WorkingDirectory $script:FrontendDirectory -Environment $frontendEnvironment
  $script:FrontendStarted = $true
  Write-ProcessRecord -Process $script:FrontendProcess -Path $script:FrontendRecordPath -WorkingDirectory $script:FrontendDirectory -CommandLineMarker $script:ViteConfig
}

function Show-ServiceAddresses {
  param([Parameter(Mandatory)][hashtable]$Configuration)
  $backendPort = [string]$Configuration['HTTP_PORT']
  $rabbitManagementPort = [string]$Configuration['RABBITMQ_MANAGEMENT_PORT']
  Write-Host ''
  Write-Host 'GoPulse development services:'
  Write-Host '  Frontend:            http://localhost:5173'
  Write-Host "  Backend:             http://localhost:$backendPort"
  Write-Host "  Health:              http://localhost:$backendPort/health"
  Write-Host "  Readiness:           http://localhost:$backendPort/ready"
  Write-Host "  RabbitMQ management: http://localhost:$rabbitManagementPort"
  Write-Host ''
  Write-Info 'Press Ctrl+C to stop Frontend and Backend. Infrastructure will remain running.'
}

function Wait-ForApplications {
  while ($true) {
    Start-Sleep -Milliseconds 500
    $script:BackendProcess.Refresh()
    if ($script:BackendProcess.HasExited) { Throw-DevError "Backend exited unexpectedly with code $($script:BackendProcess.ExitCode)." }
    $script:FrontendProcess.Refresh()
    if ($script:FrontendProcess.HasExited) { Throw-DevError "Frontend exited unexpectedly with code $($script:FrontendProcess.ExitCode)." }
  }
}

function Invoke-DevelopmentSession {
  Test-RequiredTools
  Acquire-RunLock
  Remove-Or-RejectExistingRecord -Name 'Backend' -Path $script:BackendRecordPath -ExpectedWorkingDirectory $script:BackendDirectory -ExpectedMarker $script:BackendBinary -ExpectedExecutablePath $script:BackendBinary
  Remove-Or-RejectExistingRecord -Name 'Frontend' -Path $script:FrontendRecordPath -ExpectedWorkingDirectory $script:FrontendDirectory -ExpectedMarker $script:ViteConfig -ExpectedExecutablePath $script:NodeCommand

  $configurationSource = if (Test-Path -LiteralPath $script:EnvFile -PathType Leaf) { $script:EnvFile } else { $script:EnvExampleFile }
  if (-not (Test-Path -LiteralPath $configurationSource -PathType Leaf)) { Throw-DevError '.env is absent and .env.example is not available; no local environment file was created.' }
  $preflightConfiguration = Resolve-Configuration -DotEnv (Read-DotEnv -Path $configurationSource)
  Set-ComposeEnvironment -Configuration $preflightConfiguration
  Test-RequiredPorts -Configuration $preflightConfiguration

  if (-not (Test-Path -LiteralPath $script:EnvFile -PathType Leaf)) {
    Copy-Item -LiteralPath $script:EnvExampleFile -Destination $script:EnvFile
    Write-Info 'Created .env from .env.example.'
  }
  $configuration = Resolve-Configuration -DotEnv (Read-DotEnv -Path $script:EnvFile)
  Set-ComposeEnvironment -Configuration $configuration

  Write-Info 'Starting Compose infrastructure.'
  Invoke-ComposeVisible -CommandArguments @('up', '-d', 'mysql', 'redis', 'rabbitmq')
  Wait-ForInfrastructure
  Invoke-DatabaseMigrations -Configuration $configuration
  Ensure-FrontendDependencies
  Build-Backend
  Start-Applications -Configuration $configuration
  Show-ServiceAddresses -Configuration $configuration
  Wait-ForApplications
}

$exitCode = 0
try {
  Invoke-DevelopmentSession
} catch [System.Management.Automation.PipelineStoppedException] {
  $exitCode = 130
} catch {
  [Console]::Error.WriteLine("[gopulse] ERROR: $($_.Exception.Message)")
  $exitCode = 1
} finally {
  if ($script:FrontendStarted) {
    Stop-RecordedApplication -Name 'Frontend' -Path $script:FrontendRecordPath -ExpectedWorkingDirectory $script:FrontendDirectory -ExpectedMarker $script:ViteConfig -ExpectedExecutablePath $script:NodeCommand -FallbackProcess $script:FrontendProcess
  }
  if ($script:BackendStarted) {
    Stop-RecordedApplication -Name 'Backend' -Path $script:BackendRecordPath -ExpectedWorkingDirectory $script:BackendDirectory -ExpectedMarker $script:BackendBinary -ExpectedExecutablePath $script:BackendBinary -FallbackProcess $script:BackendProcess
  }
  Release-RunLock
}
exit $exitCode
