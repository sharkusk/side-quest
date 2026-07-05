@echo off
setlocal enabledelayedexpansion
set "REPO=sharkusk/side-quest"
rem Mark every binary we exec as plugin-launched so detection stays reliable when we
rem exec a dev build on PATH outside the plugin tree (step 2) or CLAUDE_PLUGIN_DATA
rem has not propagated here (SQ-0072).
set "SIDE_QUEST_PLUGIN=1"
set "ROOT=%~dp0.."
set /p VERSION=<"%ROOT%\VERSION" 2>nul
if "%VERSION%"=="" set "VERSION=dev"
rem The binary lives in the plugin's persistent data dir — the stable, documented
rem location the terminal launcher also resolves (spec D2). CLAUDE_PLUGIN_DATA is set
rem only in Claude's own processes, so reconstruct the same deterministic path in a
rem plain shell; both launchers must agree or a provisioned binary is invisible to
rem the other (SQ-0079).
if defined CLAUDE_PLUGIN_DATA (set "DATA=%CLAUDE_PLUGIN_DATA%") else (set "DATA=%USERPROFILE%\.claude\plugins\data\side-quest-side-quest")
set "CACHE=%DATA%\bin"
set "BIN=%CACHE%\side-quest-%VERSION%.exe"

rem 1. Cached binary for this version.
if exist "%BIN%" (
  "%BIN%" %*
  exit /b %errorlevel%
)

rem 2. A side-quest already on PATH (dev build / go install), not this launcher.
rem Match only side-quest.exe so the sibling extensionless POSIX launcher and
rem this .cmd (both on the plugin bin/ PATH) are never picked by mistake.
for /f "delims=" %%p in ('where side-quest.exe 2^>nul') do (
  if /I not "%%~fp"=="%~f0" (
    "%%~fp" %*
    exit /b %errorlevel%
  )
)

rem 3. Download + checksum-verify via PowerShell (skipped when VERSION=dev).
if not "%VERSION%"=="dev" (
  set "ASSET=side-quest_%VERSION%_windows_amd64.zip"
  powershell -NoProfile -ExecutionPolicy Bypass -Command ^
    "$ErrorActionPreference='Stop';" ^
    "$base='https://github.com/%REPO%/releases/download/v%VERSION%';" ^
    "New-Item -Force -ItemType Directory '%CACHE%' | Out-Null;" ^
    "$tmp=New-TemporaryFile; Invoke-WebRequest \"$base/%ASSET%\" -OutFile \"$tmp.zip\";" ^
    "Invoke-WebRequest \"$base/checksums.txt\" -OutFile \"$tmp.sums\";" ^
    "$want=(Select-String -Path \"$tmp.sums\" -Pattern ([regex]::Escape('%ASSET%'))).Line.Split(' ')[0];" ^
    "$got=(Get-FileHash \"$tmp.zip\" -Algorithm SHA256).Hash.ToLower();" ^
    "if ($want -ne $got) { exit 3 };" ^
    "Expand-Archive \"$tmp.zip\" -DestinationPath \"$tmp.dir\" -Force;" ^
    "Move-Item -Force \"$tmp.dir\\side-quest.exe\" '%BIN%'"
  if exist "%BIN%" (
    "%BIN%" %*
    exit /b %errorlevel%
  )
)

rem 4. Could not provision — hint and fail.
echo side-quest: could not locate or download the side-quest binary.>&2
echo   Install it with:  go install github.com/%REPO%/cmd/side-quest@latest>&2
echo   or download a release from https://github.com/%REPO%/releases>&2
exit /b 1
