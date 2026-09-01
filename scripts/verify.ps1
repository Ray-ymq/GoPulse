[CmdletBinding()]
param()

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$script:RepoRoot = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$script:EnvFile = Join-Path $script:RepoRoot '.env'
$script:ProjectName = 'gopulse'
$script:Failures = [System.Collections.Generic.List[string]]::new()

function Write-Info {
  param([Parameter(Mandatory)][string]$Message)
  Write-Host "[gopulse] $Message"
}

function Add-Failure {
  param([Parameter(Mandatory)][string]$Name, [Parameter(Mandatory)][string]$Message)
  $script:Failures.Add("$Name`: $Message")
  [Console]::Error.WriteLine("[gopulse] FAIL $Name - $Message")
}

function Add-Pass {
  param([Parameter(Mandatory)][string]$Name, [Parameter(Mandatory)][string]$Message)
  Write-Info "PASS $Name - $Message"
}

function Get-DotEnvValue {
  param([Parameter(Mandatory)][string]$Key)
  if (-not (Test-Path -LiteralPath $script:EnvFile -PathType Leaf)) { return $null }

  $lineNumber = 0
  foreach ($rawLine in [System.IO.File]::ReadAllLines($script:EnvFile)) {
    $lineNumber++
    $line = $rawLine.Trim()
    if ($line.Length -eq 0 -or $line.StartsWith('#')) { continue }
    if ($line -match '^export(?:\s|$)') { throw "Unsupported dotenv syntax at line ${lineNumber}: export is not allowed." }
    if ($line -notmatch '^([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)$') { throw "Invalid dotenv assignment at line $lineNumber." }
    if ($Matches[1] -ne $Key) { continue }

    $value = $Matches[2].Trim()
    if ($value.Length -ge 1 -and ($value[0] -eq "'" -or $value[0] -eq '"')) {
      $quote = $value[0]
      if ($value.Length -lt 2 -or $value[$value.Length - 1] -ne $quote) { throw "Unterminated quoted dotenv value for $Key at line $lineNumber." }
      $value = $value.Substring(1, $value.Length - 2)
      if ($value.Contains([string]$quote)) { throw "Embedded quote syntax is not supported for $Key at line $lineNumber." }
    } elseif ($value.Contains("'") -or $value.Contains('"')) {
      throw "Mismatched quote in dotenv value for $Key at line $lineNumber."
    }
    return $value
  }
  return $null
}

function Get-HttpPort {
  $value = [System.Environment]::GetEnvironmentVariable('HTTP_PORT', 'Process')
  if ([string]::IsNullOrWhiteSpace($value)) { $value = Get-DotEnvValue -Key 'HTTP_PORT' }
  if ([string]::IsNullOrWhiteSpace($value)) { $value = '8080' }

  $port = 0
  if (-not [int]::TryParse($value, [ref]$port) -or $port -lt 1 -or $port -gt 65535) {
    throw "HTTP_PORT must be an integer from 1 to 65535; received '$value'."
  }
  return $port
}

function Assert-RequiredTools {
  $script:DockerCommand = (Get-Command docker -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1).Source
  if ([string]::IsNullOrWhiteSpace($script:DockerCommand)) { throw 'Docker is required to inspect Compose infrastructure.' }
  & $script:DockerCommand compose version *> $null
  if ($LASTEXITCODE -ne 0) { throw 'Docker Compose is unavailable.' }
  & $script:DockerCommand info *> $null
  if ($LASTEXITCODE -ne 0) { throw 'Docker is installed, but the Docker daemon is unavailable.' }
}

function Test-ComposeService {
  param([Parameter(Mandatory)][string]$Service)
  $ids = @(& $script:DockerCommand ps -a --filter "label=com.docker.compose.project=$script:ProjectName" --filter "label=com.docker.compose.service=$Service" --format '{{.ID}}' 2>$null)
  if ($LASTEXITCODE -ne 0) {
    Add-Failure -Name "Compose/$Service" -Message 'Docker could not list the service container.'
    return
  }
  $ids = @($ids | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
  if ($ids.Count -ne 1) {
    Add-Failure -Name "Compose/$Service" -Message "expected exactly one container, found $($ids.Count). Inspect with: docker compose --project-name gopulse --file `"$($script:RepoRoot)\deploy\compose.yaml`" ps"
    return
  }

  $state = (& $script:DockerCommand inspect --format '{{.State.Status}}|{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' $ids[0] 2>$null).Trim()
  if ($LASTEXITCODE -ne 0) {
    Add-Failure -Name "Compose/$Service" -Message "could not inspect container $($ids[0])."
    return
  }
  $parts = $state.Split('|', 2)
  if ($parts.Count -ne 2 -or $parts[0] -ne 'running' -or $parts[1] -ne 'healthy') {
    Add-Failure -Name "Compose/$Service" -Message "container state is '$state', expected 'running|healthy'."
    return
  }
  Add-Pass -Name "Compose/$Service" -Message 'container is running and healthy.'
}

function Invoke-HttpCheck {
  param([Parameter(Mandatory)][string]$Url, [switch]$AcceptJson)
  $handler = [System.Net.Http.HttpClientHandler]::new()
  $client = [System.Net.Http.HttpClient]::new($handler)
  $client.Timeout = [TimeSpan]::FromSeconds(5)
  try {
    $request = [System.Net.Http.HttpRequestMessage]::new([System.Net.Http.HttpMethod]::Get, $Url)
    if ($AcceptJson) { $request.Headers.Accept.ParseAdd('application/json') }
    $response = $client.SendAsync($request).GetAwaiter().GetResult()
    $body = $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
    return [pscustomobject]@{ StatusCode = [int]$response.StatusCode; Body = $body }
  } finally {
    $client.Dispose()
    $handler.Dispose()
  }
}

function Test-HealthEndpoint {
  param([Parameter(Mandatory)][int]$Port)
  $url = "http://localhost:$Port/health"
  try {
    $response = Invoke-HttpCheck -Url $url -AcceptJson
    if ($response.StatusCode -ne 200) { Add-Failure -Name '/health' -Message "returned HTTP $($response.StatusCode), expected 200."; return }
    try { $payload = $response.Body | ConvertFrom-Json -ErrorAction Stop } catch { Add-Failure -Name '/health' -Message 'did not return valid JSON.'; return }
    if ($payload.status -ne 'ok' -or $payload.service -ne 'backend') { Add-Failure -Name '/health' -Message "JSON contract mismatch (expected status=ok and service=backend)."; return }
    Add-Pass -Name '/health' -Message 'HTTP 200 with the expected JSON contract.'
  } catch {
    Add-Failure -Name '/health' -Message "request failed: $($_.Exception.Message)"
  }
}

function Test-ReadyEndpoint {
  param([Parameter(Mandatory)][int]$Port)
  $url = "http://localhost:$Port/ready"
  try {
    $response = Invoke-HttpCheck -Url $url -AcceptJson
    if ($response.StatusCode -ne 200) { Add-Failure -Name '/ready' -Message "returned HTTP $($response.StatusCode), expected 200."; return }
    try { $payload = $response.Body | ConvertFrom-Json -ErrorAction Stop } catch { Add-Failure -Name '/ready' -Message 'did not return valid JSON.'; return }
    $checks = $payload.checks
    if ($payload.status -ne 'ready' -or $payload.service -ne 'backend' -or $null -eq $checks -or $checks.mysql -ne 'up' -or $checks.redis -ne 'up' -or $checks.rabbitmq -ne 'up') {
      Add-Failure -Name '/ready' -Message 'JSON contract mismatch (expected ready backend with mysql, redis, and rabbitmq up).'
      return
    }
    Add-Pass -Name '/ready' -Message 'HTTP 200 with all dependency checks up.'
  } catch {
    Add-Failure -Name '/ready' -Message "request failed: $($_.Exception.Message)"
  }
}

function Test-Frontend {
  $url = 'http://localhost:5173/'
  try {
    $response = Invoke-HttpCheck -Url $url
    if ($response.StatusCode -lt 200 -or $response.StatusCode -ge 300) { Add-Failure -Name 'Frontend' -Message "returned HTTP $($response.StatusCode), expected a 2xx response."; return }
    Add-Pass -Name 'Frontend' -Message "HTTP $($response.StatusCode) from $url."
  } catch {
    Add-Failure -Name 'Frontend' -Message "request failed: $($_.Exception.Message)"
  }
}

try {
  Assert-RequiredTools
  $httpPort = Get-HttpPort
  Write-Info "Verifying the running environment from $script:RepoRoot."
  foreach ($service in @('mysql', 'redis', 'rabbitmq')) { Test-ComposeService -Service $service }
  Test-HealthEndpoint -Port $httpPort
  Test-ReadyEndpoint -Port $httpPort
  Test-Frontend

  if ($script:Failures.Count -gt 0) {
    [Console]::Error.WriteLine("[gopulse] Verification failed with $($script:Failures.Count) issue(s). The script did not change the running environment.")
    [Console]::Error.WriteLine('[gopulse] Diagnose Compose with: docker compose --project-name gopulse --file deploy/compose.yaml ps')
    exit 1
  }
  Write-Info 'Verification passed. The script did not change the running environment.'
  exit 0
} catch {
  [Console]::Error.WriteLine("[gopulse] ERROR: $($_.Exception.Message)")
  exit 1
}
