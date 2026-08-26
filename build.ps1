# FanControl build script for Windows (PowerShell). The Makefile covers
# Linux/rig builds; use this on the Windows dev box.
param(
    [ValidateSet('native','linux','test','check')]
    [string]$Target = 'native'
)

# NOTE: deliberately do NOT set $ErrorActionPreference='Stop' here. That setting
# makes PowerShell 7.2+ turn native commands' benign stderr (npm/esbuild install
# warnings) into terminating NativeCommandError exceptions. We instead catch
# real failures via $LASTEXITCODE checks and explicit `throw` at each step.
Set-Location (Join-Path $PSScriptRoot '.')

# Ensure the Go toolchain is on PATH regardless of how this script was launched.
# Go may have been installed after the current shell started, so the process
# PATH may be stale. Detect the install dir from the Machine/User PATH or the
# well-known default location and prepend it.
function Add-GoToPath {
    $existing = (Get-Command go -ErrorAction SilentlyContinue)
    if ($existing) { return }
    $candidates = @(
        'C:\Program Files\Go\bin',
        'C:\Go\bin',
        "$env:LOCALAPPDATA\Programs\Go\bin",
        (Join-Path $env:USERPROFILE 'go\bin')
    )
    $goPaths = @(
        ([Environment]::GetEnvironmentVariable('Path', 'Machine') -split ';') +
        ([Environment]::GetEnvironmentVariable('Path', 'User') -split ';')
    ) | Where-Object { $_ -and ($_ -match '[\\/]Go[\\/]bin$') } | Select-Object -Unique
    foreach ($p in ($candidates + $goPaths)) {
        if ($p -and (Test-Path (Join-Path $p 'go.exe'))) {
            $env:Path = $p + ';' + $env:Path
            return
        }
    }
    throw 'Go toolchain not found. Install Go (winget install GoLang.Go) or set PATH.'
}
Add-GoToPath
if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw 'go not found on PATH after detection' }

# Ensure web/dist is built (SPA must exist for the Go //go:embed).
function Build-Web {
    Write-Host '==> Building web SPA (vite)...'
    Push-Location web
    # Suppress the esbuild/npm install-script warning noise (stderr) and rely on
    # $LASTEXITCODE to detect real failures. npm.cmd is the Windows shim.
    & npm install *> $null
    if ($LASTEXITCODE -ne 0) { Pop-Location; throw "npm install failed (exit $LASTEXITCODE)" }
    & npm run build *> $null
    if ($LASTEXITCODE -ne 0) { Pop-Location; throw "vite build failed (exit $LASTEXITCODE)" }
    Pop-Location
}

switch ($Target) {
    'native' {
        Build-Web
        Write-Host '==> Building native binary (fanctrl.exe)...'
        go build -o fanctrl.exe ./cmd/fanctrl
        if ($LASTEXITCODE -ne 0) { throw 'go build failed' }
        Write-Host 'done: ./fanctrl.exe'
    }
    'linux' {
        Build-Web
        Write-Host '==> Cross-compiling linux/amd64 binary...'
        $env:GOOS = 'linux'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
        New-Item -ItemType Directory -Force -Path deploy | Out-Null
        go build -ldflags '-s -w' -o deploy/fanctrl-linux-amd64 ./cmd/fanctrl
        if ($LASTEXITCODE -ne 0) { throw 'cross-build failed' }
        Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue
        Write-Host 'done: deploy/fanctrl-linux-amd64'
    }
    'test' {
        Write-Host '==> go test ./...'
        go test ./...
    }
    'check' {
        Write-Host '==> go vet ./...'
        go vet ./...
        Write-Host '==> go test ./...'
        go test ./...
    }
}
