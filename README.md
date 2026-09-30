# Anubis RTX 4090 benchmark

[Benchmark findings](reports/findings.md)

Native Windows CUDA solver against an isolated `nginx → Anubis → backend` Docker stack on `192.168.86.54`. The default sweep runs 32 independent sessions at each `fast` difficulty 4–8 for 600 seconds, after 30 seconds of GPU warmup. Every completed session requires a fresh challenge, a CPU-verified GPU proof, an authentication cookie, and the backend's exact success response.

## Build

Requires Go 1.23+, Visual Studio 2022 C++ Build Tools, CUDA Toolkit 12.9 compiler/runtime, an Ada GPU, and NVIDIA drivers. CUDA and Microsoft build tools were installed/repaired on this machine; the driver is unchanged. Run from PowerShell:

```powershell
./scripts/build.ps1
go test ./cmd/anubis-bench
```

The build first executes a native CUDA probe, then checks SHA-256 padding, decimal nonce boundaries, and exact search results against Go SHA-256. The GPU batches hash every assigned nonce, even after finding a proof, so attempt counts include batch overrun and reflect actual hashes executed. `gpu_hashes_per_second` includes batch preparation, transfers and synchronization; `wall_hashes_per_second` additionally includes idle time waiting for HTTP requests.

## Run

SSH key access for `william@192.168.86.54`, remote Docker/Compose, and `nvidia-smi` on Windows are required. Host and local loopback port 18080 must be free.

```powershell
# Short end-to-end load validation
./scripts/run.ps1 -Difficulties 4 -SecondsPerDifficulty 15 -WarmupSeconds 2

# Full sustained sweep (approximately 50 minutes plus smoke tests)
./scripts/run.ps1

# Correctness only, for all levels
./scripts/run.ps1 -Mode Smoke
```

Runs have unique folders under `reports/`. Each stage contains session CSV, summary JSON, GPU CSV telemetry, container JSONL telemetry, logs, and Anubis Prometheus metrics. Quantiles describe accepted sessions; unfinished sessions are counted separately and have no inferred completion times. A stage ends at its deadline, so hard difficulties may have few completions. HTTP timeouts are 30 seconds; solving is bounded by the stage deadline. Any correctness, service, CUDA, or instrumentation failure stops the sweep without unlimited retries.

The runner stops the three dedicated containers and its SSH/telemetry processes on completion or failure. Reports and remote configs remain for inspection. PowerShell interruption executes cleanup; force-killing PowerShell can leave resources behind. Use the commands below to recover. Stop only the SSH PID belonging to this run if a tunnel remains.

```powershell
./scripts/server.ps1 -Action Start -Difficulty 4
./scripts/server.ps1 -Action SetDifficulty -Difficulty 5
./scripts/server.ps1 -Action Verify
./scripts/server.ps1 -Action Collect -OutDir ./reports/manual
./scripts/server.ps1 -Action Stop
```

## Isolation and security

Only nginx publishes a port, bound to remote `127.0.0.1:18080`. Windows connects through an SSH tunnel with normal host-key verification. Anubis and the backend use an internal Docker network; container CPU and memory limits are in Compose. The existing services are outside the `anubis-4090-bench` project. All images are pinned by version and digest. SELinux private labels apply only to these benchmark config files.

This dedicated test uses HTTP inside the SSH tunnel and Docker network, with `COOKIE_SECURE=false` so a normal cookie jar can exercise the protocol. Transport between machines is encrypted by SSH. This setting is **for the isolated benchmark only**; production deployments should use validated HTTPS and Secure cookies. The test policy challenges all clients and disables the honeypot/DNS reputation checks to isolate PoW. nginx has no proxy cache and omits query strings and cookie values from access logs. No signing secrets are hardcoded; Anubis generates a fresh key on restart.

Only `fast` is supported; other algorithms have different difficulty semantics. This measures a native CUDA solver, not browser JavaScript performance. No GPU clocks, power limits, drivers, host firewall or existing service configurations are changed.

nginx reuses upstream connections. The backend uses a Unix socket shared only with Anubis, using Anubis's built-in Unix transport. This avoids the pinned release's small backend idle-connection pool exhausting TCP source ports at low difficulties; no host TCP settings are modified. The ephemeral socket volume is removed during cleanup.

Anubis retains challenges for 30 minutes, including spent proofs. Its container is capped at 4 GiB with a 3 GiB Go memory target to accommodate the sustained low-difficulty workload; challenge retention and validation are unchanged. Session details are stored in CSV; summary JSON contains aggregate metrics.

The server's Docker daemon rotates container logs (currently five 10 MB files per container), so `server.log` is a retained window at high request rates. Client session CSV and Prometheus counter snapshots cover the whole stage. Telemetry measures total GPU activity, including other applications; hash counts come directly from this solver's completed CUDA batches.
