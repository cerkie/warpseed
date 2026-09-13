# update.ps1 — pull the latest warpseed and build it (run from anywhere).
# The build machine is not necessarily the machine that runs the app, so
# this only builds; pass -Run when you do want it launched afterwards.
#
# The transfer harness is built alongside the app, into the same folder as
# warpseed.exe, so it can never be a stale copy of an older commit — the one
# way a test tool misleads worse than not existing at all. It costs a couple
# of seconds; -NoHarness skips it.
#
# Usage:  & C:\path\to\warpseed\update.ps1              # update + build both
#         & C:\path\to\warpseed\update.ps1 -Run         # ... and launch the app
#         & C:\path\to\warpseed\update.ps1 -Harness     # ... and run the self-test
#         & C:\path\to\warpseed\update.ps1 -NoHarness   # app only
param(
    [switch]$Run,
    [switch]$Harness,
    [switch]$NoHarness
)

$ErrorActionPreference = "Stop"
$repo = Split-Path -Parent $MyInvocation.MyCommand.Path

if ($Harness -and $NoHarness) { throw "-Harness and -NoHarness contradict each other" }

Push-Location $repo
try {
    Write-Host "== warpseed update ==" -ForegroundColor Cyan

    # Discard build-regenerated files (wailsjs bindings, go.mod churn) —
    # this checkout never has hand edits; the Spark is the only committer.
    git checkout -- .
    git pull --ff-only
    if ($LASTEXITCODE -ne 0) { throw "git pull failed" }

    Write-Host ("Now at: " + (git log --oneline -1)) -ForegroundColor Cyan

    wails build
    if ($LASTEXITCODE -ne 0) { throw "wails build failed" }

    $exe = Join-Path $repo "build\bin\warpseed.exe"
    Write-Host "Built: $exe" -ForegroundColor Green

    $harnessExe = Join-Path $repo "build\bin\harness.exe"
    if (-not $NoHarness) {
        go build -o $harnessExe ./cmd/harness
        if ($LASTEXITCODE -ne 0) { throw "harness build failed" }
        Write-Host "Built: $harnessExe" -ForegroundColor Green
        if (-not $Harness) {
            Write-Host "Exercise real transfers on this machine with:" -ForegroundColor DarkGray
            Write-Host "  & `"$harnessExe`" -self-test" -ForegroundColor DarkGray
        }
    }

    # Show the result: the exe is what gets copied or zipped next, so open
    # its folder with the file selected rather than making the user hunt.
    explorer.exe "/select,`"$exe`""

    # The harness runs BEFORE the app is launched. It is the thing that says
    # whether this build moves bytes correctly, and reading that after the
    # window is already open invites trusting the window instead.
    if ($Harness) {
        Write-Host "== harness self-test ==" -ForegroundColor Cyan
        & $harnessExe -self-test
        if ($LASTEXITCODE -ne 0) {
            throw "harness FAILED — see warpseed-harness.log, and send it to warpseed@zyralabs.tech"
        }
        Write-Host "Harness passed." -ForegroundColor Green
    }

    if ($Run) {
        Start-Process $exe
    }
}
finally {
    Pop-Location
}
