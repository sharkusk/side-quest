# Provision native side-quest.exe into the plugin's data dir, where the MCP server
# command (${CLAUDE_PLUGIN_DATA}/bin/side-quest.exe in plugin.json) spawns it. Run by
# the plugin's SessionStart hook on Windows; the macOS/Linux arm is provision.sh.
#
# Claude spawns a plugin's MCP command with no shell, so on a native-installer Windows
# box (no `node` on PATH, nothing to run launch.js) it can only launch a real .exe by
# absolute path. So the MCP command points straight at the provisioned binary and this
# hook puts it there ahead of the spawn (SQ-0081/0089 supersede the node launcher).
#
# Idempotent (a version marker skips re-download) and non-fatal by construction: any
# failure is swallowed so it can never break session start — the MCP spawn surfaces a
# missing binary and a reconnect after the next successful run recovers.
#
# Pure .NET on purpose: this box's PowerShell may lack New-TemporaryFile / Get-FileHash
# (they raised CommandNotFoundException on the real machine — SQ-0085).
$ErrorActionPreference = 'Stop'
try {
	$repo = 'sharkusk/side-quest'
	# This script is <root>/scripts/provision.ps1; VERSION ships at the plugin root.
	$root = Split-Path -Parent $PSScriptRoot
	$version = Get-Content (Join-Path $root 'VERSION') -ErrorAction SilentlyContinue | Select-Object -First 1
	if (-not $version) { $version = 'dev' }
	$version = $version.Trim()

	# Must equal the MCP command's path and the terminal launcher's path (SQ-0079).
	$data = if ($env:CLAUDE_PLUGIN_DATA) { $env:CLAUDE_PLUGIN_DATA }
	else { Join-Path $env:USERPROFILE '.claude\plugins\data\side-quest-side-quest' }
	$bindir = Join-Path $data 'bin'
	$target = Join-Path $bindir 'side-quest.exe'
	$marker = Join-Path $bindir '.provisioned-version'

	# Already provisioned for this version.
	if ((Test-Path $target) -and (Test-Path $marker) -and
		((Get-Content $marker -Raw).Trim() -eq $version)) { exit 0 }
	# A dev checkout has no release to download from.
	if ($version -eq 'dev') { exit 0 }

	$asset = "side-quest_${version}_windows_amd64.zip"
	# SIDE_QUEST_RELEASE_BASE overrides the download host — tests + air-gapped mirrors (SQ-0084).
	$base = if ($env:SIDE_QUEST_RELEASE_BASE) { $env:SIDE_QUEST_RELEASE_BASE }
	else { "https://github.com/$repo/releases/download/v$version" }

	$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
	New-Item -ItemType Directory -Force $tmp | Out-Null
	try {
		$zip = Join-Path $tmp 'a.zip'
		$sums = Join-Path $tmp 'checksums.txt'
		Invoke-WebRequest -UseBasicParsing "$base/$asset" -OutFile $zip
		Invoke-WebRequest -UseBasicParsing "$base/checksums.txt" -OutFile $sums

		$text = [System.IO.File]::ReadAllText($sums)
		if ($text -match ('([0-9a-f]{64})\s+' + [regex]::Escape($asset))) { $want = $matches[1] }
		else { exit 0 }
		$got = ([System.BitConverter]::ToString(
				[System.Security.Cryptography.SHA256]::Create().ComputeHash(
					[System.IO.File]::ReadAllBytes($zip))) -replace '-', '').ToLower()
		if ($want -ne $got) { exit 0 }

		$ex = Join-Path $tmp 'x'
		Add-Type -AssemblyName System.IO.Compression.FileSystem
		[System.IO.Compression.ZipFile]::ExtractToDirectory($zip, $ex)
		New-Item -ItemType Directory -Force $bindir | Out-Null
		Move-Item -Force (Join-Path $ex 'side-quest.exe') $target
		[System.IO.File]::WriteAllText($marker, $version)
	}
	finally { Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue }
}
catch {
	[Console]::Error.WriteLine("side-quest provision: $_")
}
exit 0
