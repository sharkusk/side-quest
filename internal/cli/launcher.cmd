@echo off
setlocal enabledelayedexpansion
rem side-quest-cli-launcher - read-only resolver for the plugin-provisioned binary.
rem Never downloads. Windows note: a running .cmd cannot reliably delete itself, so
rem when the plugin is gone it prints its path as safe to remove (no self-delete).
rem Mark the binary we exec as plugin-launched so onboard skips .mcp.json (SQ-0072).
set "SIDE_QUEST_PLUGIN=1"
if defined CLAUDE_PLUGIN_DATA (set "DATA=%CLAUDE_PLUGIN_DATA%") else (set "DATA=%USERPROFILE%\.claude\plugins\data\side-quest-side-quest")
set "BINDIR=%DATA%\bin"

rem 1. newest provisioned binary wins.
set "NEWEST="
if exist "%BINDIR%\" (
  for /f "delims=" %%f in ('dir /b /o-d "%BINDIR%\side-quest-*.exe" 2^>nul') do (
    if not defined NEWEST set "NEWEST=%BINDIR%\%%f"
  )
)
if defined NEWEST (
  "!NEWEST!" %*
  exit /b !errorlevel!
)

rem 2. data dir present but no binary yet.
if exist "%DATA%\" (
  echo side-quest: binary not found - open a Claude Code session to finish setup.>&2
  exit /b 1
)

rem 3. data dir absent => plugin gone; inert launcher, safe to remove.
echo side-quest: the plugin is gone; this launcher is inert - safe to remove: del "%~f0">&2
exit /b 1
