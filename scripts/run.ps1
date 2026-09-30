param(
    [ValidateSet('Smoke','Benchmark')][string]$Mode = 'Benchmark',
    [ValidateRange(1,3600)][int]$SecondsPerDifficulty = 600,
    [ValidateRange(1,32)][int]$Clients = 32,
    [int[]]$Difficulties = @(4,5,6,7,8),
    [ValidateRange(1,120)][int]$WarmupSeconds = 30
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
foreach ($difficulty in $Difficulties) { if ($difficulty -lt 4 -or $difficulty -gt 8) { throw 'Difficulties must be 4..8.' } }
if (!$Difficulties.Count -or ($Difficulties | Select-Object -Unique).Count -ne $Difficulties.Count) { throw 'Choose distinct difficulties.' }
$exe = Join-Path $root 'build/anubis-bench.exe'
if (!(Test-Path $exe)) { throw 'Run scripts/build.ps1 first.' }
$runDir = Join-Path $root ("reports/" + [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ') + '-' + [Guid]::NewGuid().ToString('N').Substring(0,8))
New-Item -ItemType Directory -Force $runDir | Out-Null
@{seconds=$SecondsPerDifficulty;clients=$Clients;difficulties=$Difficulties;warmup_seconds=$WarmupSeconds;target='192.168.86.54';mode=$Mode} | ConvertTo-Json | Set-Content "$runDir/run.json" -Encoding utf8
$tunnel = $null
$started = $false
$failure = $null
try {
    & $exe -mode selftest | Tee-Object "$runDir/selftest.log"
    if ($LASTEXITCODE -ne 0) { throw 'GPU correctness test failed.' }
    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback,18080)
    try { $listener.Start() } finally { $listener.Stop() }
    $started = $true
    & "$PSScriptRoot/server.ps1" -Action Start -Difficulty $Difficulties[0]
    $tunnel = Start-Process ssh.exe -ArgumentList '-N -o BatchMode=yes -o ExitOnForwardFailure=yes -o ServerAliveInterval=15 -o ServerAliveCountMax=3 -L 127.0.0.1:18080:127.0.0.1:18080 192.168.86.54' -WindowStyle Hidden -PassThru -RedirectStandardError "$runDir/tunnel.log"
    Start-Sleep -Seconds 1
    if ($tunnel.HasExited) { throw 'SSH tunnel failed; see tunnel.log.' }
    & $exe -mode warmup -seconds $WarmupSeconds | Tee-Object "$runDir/warmup.log"
    if ($LASTEXITCODE -ne 0) { throw 'GPU warmup failed.' }
    foreach ($difficulty in $Difficulties) {
        $stage = Join-Path $runDir "difficulty-$difficulty"
        New-Item -ItemType Directory -Force $stage | Out-Null
        & "$PSScriptRoot/server.ps1" -Action SetDifficulty -Difficulty $difficulty
        & $exe -mode smoke -difficulty $difficulty | Tee-Object "$stage/smoke.log"
        if ($LASTEXITCODE -ne 0) { throw "Difficulty $difficulty correctness smoke test failed." }
        if ($Mode -eq 'Smoke') { & "$PSScriptRoot/server.ps1" -Action Collect -OutDir $stage; continue }
        & ssh -o BatchMode=yes 192.168.86.54 'cd ~/anubis-4090-bench && docker compose exec -T nginx wget -qO- http://anubis:9090/metrics' | Set-Content "$stage/anubis-metrics-start.txt" -Encoding utf8
        if ($LASTEXITCODE -ne 0) { throw 'Failed to collect baseline Anubis metrics.' }
        $gpuTelemetry = $null
        $serverTelemetry = $null
        $client = $null
        try {
            $gpuTelemetry = Start-Process nvidia-smi.exe -ArgumentList '--query-gpu=timestamp,name,utilization.gpu,temperature.gpu,power.draw,memory.used --format=csv -l 1' -WindowStyle Hidden -PassThru -RedirectStandardOutput "$stage/gpu.csv" -RedirectStandardError "$stage/gpu-errors.log"
            $telemetryDuration = $SecondsPerDifficulty + 30
            $serverTelemetry = Start-Process ssh.exe -ArgumentList "-o BatchMode=yes -o ConnectTimeout=10 192.168.86.54 sh ~/anubis-4090-bench/telemetry.sh $telemetryDuration" -WindowStyle Hidden -PassThru -RedirectStandardOutput "$stage/containers.jsonl" -RedirectStandardError "$stage/container-errors.log"
            $client = Start-Process $exe -ArgumentList "-mode bench -difficulty $difficulty -clients $Clients -seconds $SecondsPerDifficulty -out `"$stage`"" -WindowStyle Hidden -PassThru -RedirectStandardOutput "$stage/client.log" -RedirectStandardError "$stage/client-errors.log"
            Write-Host "Running difficulty $difficulty for $SecondsPerDifficulty seconds with $Clients clients. Reports: $stage"
            while (!$client.WaitForExit(1000)) {
                if ($tunnel.HasExited -or $gpuTelemetry.HasExited -or $serverTelemetry.HasExited) { throw 'Tunnel or telemetry process failed; see stage error logs.' }
            }
            if ($client.ExitCode -ne 0) { throw "Difficulty $difficulty failed: $(Get-Content "$stage/client-errors.log" -Raw)" }
        } finally {
            foreach ($process in @($client,$gpuTelemetry,$serverTelemetry)) { if ($process -and !$process.HasExited) { Stop-Process -Id $process.Id -ErrorAction Stop } }
            & "$PSScriptRoot/server.ps1" -Action Collect -OutDir $stage
        }
    }
    'complete' | Set-Content "$runDir/status.txt"
} catch {
    $failure = $_
    $_ | Out-String | Set-Content "$runDir/failure.txt"
    'failed' | Set-Content "$runDir/status.txt"
} finally {
    if ($tunnel -and !$tunnel.HasExited) { Stop-Process -Id $tunnel.Id }
    if ($started) { & "$PSScriptRoot/server.ps1" -Action Stop }
    Write-Host "Reports preserved: $runDir"
}
if ($failure) { throw $failure }
