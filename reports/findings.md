# Findings: RTX 4090 against Anubis v1.27.0 `fast` proof-of-work

**Repository:** https://github.com/firebadnofire/aunbis-test  
**Primary sustained run:** `20260930T021753Z-5450bb00`

## Executive summary

The sustained benchmark completed successfully across Anubis `fast` difficulties 4 through 8 using 32 concurrent clients and 600 seconds per difficulty.

Across the full sweep, the client executed approximately **5.01 trillion SHA-256 attempts** and completed **3,470,689 server-confirmed sessions**. There were **zero unexpected rejections, zero timeouts, and zero recorded failures** in the completed stages.

The RTX 4090 reached **4.31 GH/s** during the 30-second synthetic warmup. During sustained end-to-end Anubis testing, difficulties 5 through 8 held roughly **1.96–2.05 GH/s** while the GPU remained at approximately **95–98% utilization**. Difficulty 4 was not GPU-limited: the server and HTTP/session path became the bottleneck first, leaving the GPU at only about 47% average utilization.

The results also show the expected scaling behavior of Anubis's SHA-256 proof-of-work. From difficulty 5 onward, each additional required leading hexadecimal zero reduced accepted challenge throughput by roughly **15–15.5×**, close to the theoretical **16× increase in expected work per difficulty step**.

These results demonstrate that a native CUDA implementation can solve genuine Anubis v1.27.0 `fast` challenges at multi-gigahash-per-second rates on an RTX 4090. They do **not**, by themselves, establish a specific multiplier against a phone or browser implementation because this report set contains no corresponding browser or phone baseline.

## Test configuration

The sustained run used:

- Anubis `fast` SHA-256 proof-of-work
- Difficulties: **4, 5, 6, 7, 8**
- Duration: **600 seconds per difficulty**
- Concurrent clients: **32**
- GPU warmup: **30 seconds**
- GPU: **NVIDIA GeForce RTX 4090**
- Native CUDA solver with CPU verification before proof submission
- End-to-end server confirmation required for a session to count as accepted

The run completed with status `complete`.

## Correctness validation

Before the sustained sweep, the CPU and CUDA implementations were compared across **182 correctness cases**, all of which passed.

For every tested difficulty from 4 through 8, the smoke test confirmed all of the following:

- A GPU-generated proof was accepted by Anubis.
- The request reached the expected backend.
- An invalid proof was rejected.
- A replayed proof was rejected.

This matters because the benchmark is measuring proofs accepted by the actual Anubis validation path, not merely synthetic SHA-256 throughput.

## Sustained benchmark results

| Difficulty | Expected hashes per proof | GPU rate | Accepted sessions | Accepted/s | Solve p50 | Solve p95 | Solve p99 |
|---:|---:|---:|---:|---:|---:|---:|---:|
| 4 | 65,536 | 0.654 GH/s | 2,325,347 | 3,875.56/s | 0.500 ms | 2.503 ms | 4.005 ms |
| 5 | 1,048,576 | 1.957 GH/s | 1,071,288 | 1,785.48/s | 9.533 ms | 41.092 ms | 64.191 ms |
| 6 | 16,777,216 | 1.970 GH/s | 69,276 | 115.46/s | 182.775 ms | 830.470 ms | 1.311 s |
| 7 | 268,435,456 | 2.039 GH/s | 4,476 | 7.460/s | 2.835 s | 13.384 s | 20.118 s |
| 8 | 4,294,967,296 | 2.048 GH/s | 302 | 0.503/s | 37.947 s | 168.401 s | 289.470 s |

Every completed stage recorded:

- `rejected: 0`
- `timeouts: 0`
- `failures: 0`

Each stage ended with 32 unfinished sessions, corresponding to the 32 active client slots at the stage deadline. Those unfinished sessions are reported separately rather than being assigned inferred completion times.

## Warmup throughput

The 30-second warmup produced:

```text
hashes: 129,304,100,864
hashes_per_second: 4,310,078,768
seconds: 30.0004
```

That is approximately **4.31 GH/s**.

This should be treated as a synthetic peak-style throughput measurement rather than the primary end-to-end Anubis result. The sustained Anubis tests are more representative because they include the actual challenge format, CUDA batching, CPU verification, HTTP exchange, server validation, cookies, and backend confirmation.

## Difficulty scaling

For SHA-256 PoW with difficulty measured in leading hexadecimal zeroes, the expected work increases by a factor of 16 for each additional difficulty level:

```text
difficulty 4:        65,536 expected hashes
difficulty 5:     1,048,576 expected hashes
difficulty 6:    16,777,216 expected hashes
difficulty 7:   268,435,456 expected hashes
difficulty 8: 4,294,967,296 expected hashes
```

Observed accepted-session throughput from difficulty 5 onward fell by:

```text
difficulty 5 → 6: 15.46×
difficulty 6 → 7: 15.48×
difficulty 7 → 8: 14.82×
```

That is close to the theoretical 16× step and is strong evidence that the benchmark is measuring the intended proof-of-work behavior.

Difficulty 4 does not follow that relationship because the GPU is capable of solving the PoW faster than the surrounding request path can issue, validate, and complete sessions.

## GPU utilization and thermals

| Difficulty | Avg GPU utilization | Avg temperature | Max temperature | Avg power | Max power | Avg VRAM |
|---:|---:|---:|---:|---:|---:|---:|
| 4 | 46.9% | 52.6°C | 69°C | 157.4 W | 333.2 W | 8.62 GiB |
| 5 | 95.2% | 70.7°C | 72°C | 319.7 W | 347.0 W | 8.47 GiB |
| 6 | 97.8% | 72.5°C | 74°C | 336.1 W | 362.2 W | 8.53 GiB |
| 7 | 98.1% | 72.5°C | 74°C | 336.2 W | 362.3 W | 8.62 GiB |
| 8 | 97.9% | 71.2°C | 73°C | 327.8 W | 362.1 W | 8.44 GiB |

The difficulty-4 stage is visibly underutilized compared with the higher difficulties. At difficulty 5 and above, the GPU becomes the dominant compute resource and remains nearly saturated.

## Bottleneck transition

Difficulty 4 is chiefly a server/protocol throughput test rather than a GPU throughput test.

At that level, expected work is only 65,536 hashes per proof. The GPU can complete the cryptographic work much faster than the surrounding system can continuously:

1. issue a fresh challenge,
2. transfer it to the client,
3. schedule GPU work,
4. verify the result on the CPU,
5. submit the proof,
6. validate it in Anubis,
7. establish authentication state,
8. reach the backend, and
9. begin another independent session.

This is reflected in the much lower GPU utilization and the lower reported sustained GPU hash rate at difficulty 4.

At difficulties 5 through 8, GPU utilization rises above 95% and sustained throughput stabilizes near 2 GH/s, indicating that the proof-of-work computation has become the dominant bottleneck.

## Latency behavior

Solve latency rises dramatically with difficulty because SHA-256 nonce search is probabilistic.

At difficulty 8:

- p50 solve time was **37.95 seconds**
- p95 was **168.40 seconds**
- p99 was **289.47 seconds**

The wide distribution is expected for an independent brute-force nonce search. A proof can be found immediately or require substantially more than the mean number of hashes.

The end-to-end latency closely tracks solve latency at high difficulties, which shows that protocol overhead becomes comparatively insignificant once PoW dominates the workload.

## What the benchmark establishes

The report set supports the following conclusions:

1. The CUDA solver is compatible with the tested Anubis v1.27.0 `fast` proof format and produced proofs accepted by the real server validator.
2. An RTX 4090 can sustain approximately **2.0 GH/s** while repeatedly solving real Anubis challenges under sustained GPU-bound conditions.
3. The solver's throughput follows the expected exponential difficulty scaling of the SHA-256 PoW.
4. At low difficulty, server and protocol throughput can become the limiting factor before the RTX 4090's hashing capacity is exhausted.
5. The tested system completed millions of validated sessions without unexpected proof rejection, timeout, or client failure during the completed sustained sweep.

## What the benchmark does not establish

This data should not be stretched beyond what was measured.

In particular:

- It does not measure Anubis's browser JavaScript/WASM implementation.
- It does not measure a phone.
- It does not yet provide a defensible RTX-4090-to-phone or RTX-4090-to-browser speed multiplier.
- It does not show that SHA-256 itself has been cryptographically broken.
- It does not test Anubis challenge algorithms other than `fast`.
- Difficulty 4 should not be used as the RTX 4090's maximum sustained hashing throughput because that stage was bottlenecked elsewhere.

A browser or phone baseline is required before making a quantitative claim such as "the GPU is hundreds of times faster than a phone browser."

## Conclusion

The RTX 4090 proved capable of solving Anubis v1.27.0 `fast` SHA-256 challenges at roughly **2 GH/s sustained** once difficulty was high enough to keep the GPU occupied, with a **4.31 GH/s synthetic warmup rate**.

The clean 16×-per-difficulty scaling, successful CPU/GPU cross-checks, server acceptance of generated proofs, rejection of invalid and replayed proofs, and absence of unexpected failures make the sustained results internally consistent.

The most important practical finding is that low-difficulty SHA-256 proof-of-work does not impose a comparable cost on every class of client. On sufficiently powerful parallel hardware, the PoW itself can become cheap enough that the surrounding HTTP and server machinery is the bottleneck instead.

A separate browser and phone benchmark is still necessary to quantify the hardware asymmetry directly.
