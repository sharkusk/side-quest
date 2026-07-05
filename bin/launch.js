#!/usr/bin/env node
// Cross-platform launcher for the side-quest plugin MCP server.
//
// Claude Code spawns a plugin's MCP `command` with Node's child_process (no shell),
// which on Windows does NOT honor PATHEXT — so an extensionless shim path
// (`${CLAUDE_PLUGIN_ROOT}/bin/side-quest`) can't be found there and the server fails
// (SQ-0081). `node` itself IS spawnable on every OS Claude Code runs on (Claude Code
// is a Node app and its own docs launch MCP servers via `node`/`npx`), so plugin.json
// registers this dispatcher, which hands off to the OS-appropriate provisioning shim:
// the POSIX shell shim carries a shebang and runs directly; the Windows `.cmd` must go
// through cmd.exe (Node cannot spawn a .cmd without a shell). The shims own all the
// download/provision logic — this file only picks the right one and forwards args.
'use strict';

const path = require('path');
const { spawnSync } = require('child_process');

const binDir = __dirname;
const args = process.argv.slice(2);

let res;
if (process.platform === 'win32') {
  const cmd = path.join(binDir, 'side-quest.cmd');
  res = spawnSync('cmd.exe', ['/c', cmd, ...args], { stdio: 'inherit' });
} else {
  const sh = path.join(binDir, 'side-quest');
  res = spawnSync(sh, args, { stdio: 'inherit' });
}

if (res.error) {
  process.stderr.write('side-quest launcher: ' + res.error.message + '\n');
  process.exit(1);
}
process.exit(res.status === null ? 1 : res.status);
