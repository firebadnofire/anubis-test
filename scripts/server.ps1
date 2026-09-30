param(
    [ValidateSet('Start','SetDifficulty','Verify','Collect','Stop')][string]$Action = 'Start',
    [ValidateRange(4,8)][int]$Difficulty = 4,
    [string]$OutDir
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$hostName = '192.168.86.54'
function Remote([string]$command) {
    & ssh -o BatchMode=yes -o ConnectTimeout=10 $hostName $command
    if ($LASTEXITCODE -ne 0) { throw "Remote benchmark operation failed: $command" }
}
function CopyRemote([string[]]$files) {
    & scp @files "${hostName}:anubis-4090-bench/"
    if ($LASTEXITCODE -ne 0) { throw 'Copying benchmark configuration failed.' }
}
function SetPolicy {
    New-Item -ItemType Directory -Force "$root/.state" | Out-Null
    (Get-Content "$root/server/policy.yaml" -Raw).Replace('difficulty: 4', "difficulty: $Difficulty") | Set-Content "$root/.state/policy.yaml" -Encoding utf8
    CopyRemote @("$root/.state/policy.yaml")
    Remote 'cd ~/anubis-4090-bench && chmod 644 policy.yaml && docker compose config --quiet && docker compose up -d --force-recreate --wait --wait-timeout 60'
}
switch ($Action) {
    Start {
        Remote 'mkdir -p ~/anubis-4090-bench'
        CopyRemote @("$root/server/compose.yaml", "$root/server/nginx.conf", "$root/server/backend.conf", "$root/server/telemetry.sh")
        Remote 'cd ~/anubis-4090-bench && chmod 755 . && chmod 644 compose.yaml nginx.conf backend.conf telemetry.sh'
        SetPolicy
    }
    SetDifficulty { SetPolicy }
    Verify { Remote 'cd ~/anubis-4090-bench && docker compose ps && docker compose exec -T nginx nginx -t && curl --fail --silent --show-error --max-time 10 --output /dev/null http://127.0.0.1:18080/' }
    Collect {
        if (!$OutDir) { throw 'Collect requires -OutDir.' }
        New-Item -ItemType Directory -Force $OutDir | Out-Null
        Remote 'cd ~/anubis-4090-bench && docker compose logs --no-color > benchmark-server.log'
        & scp "${hostName}:anubis-4090-bench/benchmark-server.log" "$OutDir/server.log"
        if ($LASTEXITCODE -ne 0) { throw 'Copying benchmark logs failed.' }
        Remote 'cd ~/anubis-4090-bench && docker compose exec -T nginx wget -qO- http://anubis:9090/metrics' | Set-Content "$OutDir/anubis-metrics.txt" -Encoding utf8
        Remote 'cd ~/anubis-4090-bench && docker compose ps --format json' | Set-Content "$OutDir/container-state.jsonl" -Encoding utf8
    }
    Stop { Remote 'cd ~/anubis-4090-bench && docker compose down --volumes --timeout 10' }
}
