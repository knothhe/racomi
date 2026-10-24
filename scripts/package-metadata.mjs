import fs from 'node:fs';
import {execFileSync} from 'node:child_process';
const [file, plist, channel, version, number] = process.argv.slice(2);
const info = JSON.parse(fs.readFileSync(file, 'utf8'));
const id = channel === 'dev' ? 'com.knothhe.racomi.dev' : 'com.knothhe.racomi';
if (info.channel !== channel || info.id !== id || info.version !== version || info.number !== number || info.os !== 'darwin') throw Error('binary/package identity mismatch');
const set = (key, value) => execFileSync('/usr/libexec/PlistBuddy', ['-c', `Set :${key} ${value}`, plist]);
const add = (key, type, value) => execFileSync('/usr/libexec/PlistBuddy', ['-c', `Add :${key} ${type} ${value}`, plist]);
set('CFBundleIdentifier', id); set('CFBundleName', info.name); set('CFBundleDisplayName', info.name);
for (const key of ['channel','executor','commit']) add(`Racomi${key[0].toUpperCase()+key.slice(1)}`, 'string', info[key]);
add('RacomiDirty', 'bool', String(info.dirty));
if (channel === 'stable') {
 const updates = JSON.parse(fs.readFileSync(new URL('../build/updates.json', import.meta.url)));
 add('SUFeedURL', 'string', updates.feedURL); add('SUPublicEDKey', 'string', updates.publicKey);
 add('SUEnableAutomaticChecks', 'bool', 'true'); add('SUAutomaticallyUpdate', 'bool', 'false');
 add('SURequireSignedFeed', 'bool', 'true');
 add('SUEnableInstallerLauncherService', 'bool', 'true'); add('SUVerifyUpdateBeforeExtraction', 'bool', 'true');
 add('SUSendProfileInfo', 'bool', 'false'); add('SUScheduledCheckInterval', 'integer', '86400');
}
