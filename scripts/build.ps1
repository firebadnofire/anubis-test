param([switch]$ProbeOnly)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$vswhere = "${env:ProgramFiles(x86)}/Microsoft Visual Studio/Installer/vswhere.exe"
$vs = & $vswhere -latest -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
if (!$vs) { throw 'Microsoft C++ Build Tools are missing.' }
Import-Module "$vs/Common7/Tools/Microsoft.VisualStudio.DevShell.dll"
Enter-VsDevShell -VsInstallPath $vs -SkipAutomaticLocation -DevCmdArguments '-arch=x64 -host_arch=x64'
$cuda = 'C:/Program Files/NVIDIA GPU Computing Toolkit/CUDA/v12.9'
if (!(Test-Path "$cuda/bin/nvcc.exe")) { throw 'CUDA Toolkit 12.9 compiler/runtime are required.' }
New-Item -ItemType Directory -Force "$root/build" | Out-Null
Push-Location $root
try {
    & "$cuda/bin/nvcc.exe" -O3 -arch=sm_89 -o build/probe.exe cuda/probe.cu
    if ($LASTEXITCODE -ne 0) { throw 'CUDA probe compilation failed.' }
    & ./build/probe.exe
    if ($LASTEXITCODE -ne 0) { throw 'CUDA execution probe failed.' }
    if ($ProbeOnly) { return }
    & "$cuda/bin/nvcc.exe" -O3 -arch=sm_89 --shared -Xcompiler /MT -o build/anubis_cuda.dll cuda/solver.cu
    if ($LASTEXITCODE -ne 0) { throw 'CUDA solver compilation failed.' }
    go build -o build/anubis-bench.exe ./cmd/anubis-bench
    if ($LASTEXITCODE -ne 0) { throw 'Go client compilation failed.' }
    & ./build/anubis-bench.exe -mode selftest
    if ($LASTEXITCODE -ne 0) { throw 'CPU/GPU correctness tests failed.' }
} finally { Pop-Location }
