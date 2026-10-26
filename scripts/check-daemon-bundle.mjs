import assert from 'node:assert/strict';
import {readdir, readFile} from 'node:fs/promises';

const assets = new URL('../frontend/dist-daemon/assets/', import.meta.url);
const files = (await readdir(assets)).filter(name => name.endsWith('.js'));
assert(files.length > 0, 'Missing daemon JavaScript bundle');
for (const name of files) {
  const code = await readFile(new URL(name, assets), 'utf8');
  for (const token of ['/api/settings/updater', '/api/updater/', 'CheckForUpdate', 'InstallUpdate', 'update:available', 'updater-enabled', 'updaterConfig', 'updateInfo']) {
    assert(!code.includes(token), `Desktop updater leaked into daemon bundle: ${name}: ${token}`);
  }
}
console.log('Daemon bundle contains no updater controls, requests or update event subscription.');
