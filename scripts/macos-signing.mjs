#!/usr/bin/env node
// Creates and uploads the fixed macOS code-signing identity that release.yml
// imports, and verifies release bundles against its pinned fingerprint. A
// self-signed certificate keeps the designated requirement stable across
// releases, so Keychain keeps trusting updated builds. It does not replace
// Developer ID notarization for Gatekeeper.
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import readline from 'node:readline/promises';
import {execFileSync, spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';

export const certificateSecret = 'MACOS_SIGNING_CERTIFICATE';
export const passwordSecret = 'MACOS_SIGNING_CERTIFICATE_PASSWORD';
export const defaultIdentityDir = path.join(os.homedir(), '.config/racomi-release/macos-signing');
// The committed pin names the one certificate releases may be signed with.
export const defaultPinPath = fileURLToPath(new URL('../build/macos/signing.json', import.meta.url));
const defaultRepository = 'knothhe/racomi';
const commonName = 'racomi Release Signing (self-signed)';
const codeSigningOID = '1.3.6.1.5.5.7.3.3';
const identityFiles = ['certificate.pem', 'identity.p12', 'identity-password'];
const replaceWarning = 'Installed clients ask for Keychain access once more after the first update signed with a different certificate.';

const requireThat = (condition, message) => { if (!condition) throw Error(message); };
const sha256 = (bytes) => crypto.createHash('sha256').update(bytes).digest('hex');
const run = (command, args, input) => execFileSync(command, args, {encoding: 'utf8', input, stdio: [input === undefined ? 'ignore' : 'pipe', 'pipe', 'pipe']});

export function certificateFingerprint(pem) {
 const certificate = new crypto.X509Certificate(pem);
 requireThat(certificate.keyUsage?.includes(codeSigningOID), 'Signing certificate must allow code signing.');
 requireThat(!certificate.ca, 'Signing certificate must not be a certificate authority.');
 return sha256(certificate.raw);
}

export function readPin(file = defaultPinPath) {
 if (!fs.existsSync(file)) return undefined;
 const pin = JSON.parse(fs.readFileSync(file, 'utf8'));
 requireThat(pin && typeof pin === 'object' && Object.keys(pin).sort().join() === 'certificateSHA256,schema' && pin.schema === 1 && /^[a-f0-9]{64}$/.test(pin.certificateSHA256),
  `${file} must contain only schema 1 and a certificateSHA256 fingerprint.`);
 return pin.certificateSHA256;
}

const writePin = (file, fingerprint) => fs.writeFileSync(file, `${JSON.stringify({schema: 1, certificateSHA256: fingerprint}, null, 1)}\n`);

export function assertCertificateRequirement(requirement) {
 requireThat(/designated => .*certificate (leaf|root) = H"[0-9a-f]{40}"/i.test(requirement) && !/cdhash/i.test(requirement), 'Signed bundle must use a certificate-based designated requirement.');
}

function codesignDisplay(target, ...args) {
 const result = spawnSync('/usr/bin/codesign', ['--display', ...args, target], {encoding: 'utf8'});
 requireThat(result.status === 0, `codesign could not read ${target}: ${result.stderr.trim()}`);
 return `${result.stdout}${result.stderr}`;
}

// Checks the bundle and its executables after packaging: each must carry the
// pinned certificate and a designated requirement bound to it.
export function verify(bundle, {pin = readPin(), codesign = codesignDisplay} = {}) {
 requireThat(pin, 'No pinned macOS signing certificate; run `node scripts/macos-signing.mjs` and commit build/macos/signing.json.');
 const work = fs.mkdtempSync(path.join(os.tmpdir(), 'racomi-signature-'));
 try {
  const executables = fs.readdirSync(path.join(bundle, 'Contents/MacOS')).map((name) => path.join(bundle, 'Contents/MacOS', name));
  for (const [index, target] of [bundle, ...executables].entries()) {
   const prefix = path.join(work, `${index}-`);
   codesign(target, `--extract-certificates=${prefix}`);
   requireThat(certificateFingerprint(fs.readFileSync(`${prefix}0`)) === pin, `${target} is not signed with the pinned macOS release certificate.`);
   assertCertificateRequirement(codesign(target, '--requirements', '-'));
  }
 } finally {
  fs.rmSync(work, {recursive: true, force: true});
 }
}

// Replacing an identity keeps the previous files in a sibling directory, so a
// mistaken replacement can still be undone before the secrets are overwritten.
export function generate({dir = defaultIdentityDir, replace = false, openssl = (args) => run('openssl', args)} = {}) {
 const files = identityFiles.map((name) => path.join(dir, name));
 const existing = files.filter((file) => fs.existsSync(file));
 requireThat(!existing.length || replace, `A macOS signing identity already exists in ${dir}. ${replaceWarning}`);
 fs.mkdirSync(dir, {recursive: true, mode: 0o700});
 fs.chmodSync(dir, 0o700);
 // Build the identity next to its destination so a failed run changes nothing
 // and the final renames stay on one filesystem.
 const work = fs.mkdtempSync(path.join(dir, '.generate-'));
 let replaced;
 try {
  const request = path.join(work, 'request.cnf'), key = path.join(work, 'key.pem');
  const staged = identityFiles.map((name) => path.join(work, name));
  fs.writeFileSync(request, `[req]\ndistinguished_name=dn\nx509_extensions=ext\nprompt=no\n[dn]\nCN=${commonName}\n[ext]\nbasicConstraints=critical,CA:FALSE\nkeyUsage=critical,digitalSignature\nextendedKeyUsage=critical,codeSigning\nsubjectKeyIdentifier=hash\n`);
  fs.writeFileSync(staged[2], crypto.randomBytes(32).toString('base64url'), {mode: 0o600});
  // Signing fails once the certificate expires; 30 years outlives the app.
  openssl(['req', '-x509', '-newkey', 'rsa:3072', '-nodes', '-sha256', '-days', '10950', '-config', request, '-keyout', key, '-out', staged[0]]);
  // macOS `security import` needs the legacy PKCS#12 algorithms.
  openssl(['pkcs12', '-export', '-inkey', key, '-in', staged[0], '-out', staged[1], '-name', commonName, '-passout', `file:${staged[2]}`,
   '-keypbe', 'PBE-SHA1-3DES', '-certpbe', 'PBE-SHA1-3DES', '-macalg', 'sha1']);
  certificateFingerprint(fs.readFileSync(staged[0]));
  if (existing.length) {
   replaced = path.join(dir, `replaced-${new Date().toISOString().replaceAll(':', '-')}`);
   fs.mkdirSync(replaced, {mode: 0o700});
   for (const file of existing) fs.renameSync(file, path.join(replaced, path.basename(file)));
  }
  staged.forEach((file, index) => { fs.chmodSync(file, 0o600); fs.renameSync(file, files[index]); });
 } finally {
  fs.rmSync(work, {recursive: true, force: true});
 }
 return {fingerprint: certificateFingerprint(fs.readFileSync(files[0])), dir, replaced};
}

// `security find-identity` lists untrusted self-signed identities too.
export function parseIdentityList(output) {
 const identities = new Map();
 for (const match of output.matchAll(/^\s*\d+\)\s+([0-9A-F]{40})\s+"(.*)"(?:\s+\((.+)\))?\s*$/gm)) {
  if (!identities.has(match[1])) identities.set(match[1], {hash: match[1], name: match[2], status: match[3] ?? ''});
 }
 return [...identities.values()];
}

// Picks one certificate and its private key from a PEM dump of a keychain
// export, which may contain every identity in that keychain.
export function selectIdentity(pem, sha1) {
 const blocks = [...pem.matchAll(/-----BEGIN ([A-Z ]+)-----[\s\S]+?-----END \1-----/g)];
 const spki = (key) => key.export({type: 'spki', format: 'der'}).toString('hex');
 const certificate = blocks.filter((block) => block[1] === 'CERTIFICATE').map((block) => new crypto.X509Certificate(block[0]))
  .find((item) => item.fingerprint.replaceAll(':', '') === sha1.toUpperCase());
 requireThat(certificate, 'The selected certificate is missing from the keychain export.');
 const key = blocks.filter((block) => block[1].endsWith('PRIVATE KEY')).map((block) => crypto.createPrivateKey(block[0]))
  .find((item) => spki(crypto.createPublicKey(item)) === spki(certificate.publicKey));
 requireThat(key, 'The private key of the selected certificate is missing from the keychain export.');
 return {certificate: certificate.toString(), key: key.export({type: 'pkcs8', format: 'pem'})};
}

function readIdentityDir(dir) {
 const [certificate, identity, password] = identityFiles.map((name) => path.join(dir, name));
 requireThat(identityFiles.every((name) => fs.existsSync(path.join(dir, name))), `No macOS signing identity in ${dir}.`);
 return {certificate: fs.readFileSync(certificate, 'utf8'), identity: fs.readFileSync(identity), password: fs.readFileSync(password, 'utf8').trim()};
}

// Exports one code-signing identity from the maintainer's macOS keychain.
async function readKeychainIdentity(ask, work) {
 const keychains = run('/usr/bin/security', ['list-keychains', '-d', 'user']).split('\n').map((line) => line.trim().replace(/^"|"$/g, '')).filter(Boolean);
 const identities = keychains.flatMap((keychain) => parseIdentityList(run('/usr/bin/security', ['find-identity', '-p', 'codesigning', keychain])).map((identity) => ({...identity, keychain})));
 requireThat(identities.length, 'No code-signing identity was found in the keychain search list.');
 const selected = await choose(ask, 'Which certificate?', identities.map((identity) => ({
  label: `${identity.name}${identity.status ? ` (${identity.status})` : ''}\n     ${identity.hash}  ${identity.keychain}`, value: identity})));
 // `security export` cannot pick one identity; keep the full export in a
 // private temporary directory and repack only the selected pair.
 const file = (name) => path.join(work, name);
 const password = crypto.randomBytes(32).toString('base64url');
 fs.writeFileSync(file('export-password'), crypto.randomBytes(24).toString('base64url'), {mode: 0o600});
 fs.writeFileSync(file('password'), password, {mode: 0o600});
 console.log('macOS may ask for permission to export the keychain private keys.');
 run('/usr/bin/security', ['export', '-k', selected.keychain, '-t', 'identities', '-f', 'pkcs12', '-P', fs.readFileSync(file('export-password'), 'utf8'), '-o', file('all.p12')]);
 const pair = selectIdentity(run('/usr/bin/openssl', ['pkcs12', '-in', file('all.p12'), '-nodes', '-passin', `file:${file('export-password')}`]), selected.hash);
 fs.writeFileSync(file('certificate.pem'), pair.certificate, {mode: 0o600});
 fs.writeFileSync(file('key.pem'), pair.key, {mode: 0o600});
 run('/usr/bin/openssl', ['pkcs12', '-export', '-inkey', file('key.pem'), '-in', file('certificate.pem'), '-out', file('identity.p12'), '-name', selected.name,
  '-passout', `file:${file('password')}`, '-keypbe', 'PBE-SHA1-3DES', '-certpbe', 'PBE-SHA1-3DES']);
 return {certificate: pair.certificate, identity: fs.readFileSync(file('identity.p12')), password};
}

// Asks until the answer is one of the numbered options.
async function choose(ask, question, options) {
 options.forEach((option, index) => console.log(`  ${index + 1}) ${option.label}`));
 for (let attempt = 0; attempt < 3; attempt++) {
  const answer = (await ask(`${question} [1-${options.length}]: `)).trim();
  if (/^\d+$/.test(answer) && options[Number(answer) - 1]) return options[Number(answer) - 1].value;
  console.log('Enter one of the listed numbers.');
 }
 throw Error('No option was chosen.');
}

const askWithDefault = async (ask, question, fallback) => (await ask(`${question} [${fallback}]: `)).trim() || fallback;
const confirmWord = async (ask, question, word) => (await ask(`${question} Type ${word} to continue: `)).trim() === word;

function describe(pem) {
 const certificate = new crypto.X509Certificate(pem);
 const expired = Date.parse(certificate.validTo) < Date.now() ? ' (expired)' : '';
 return `  Certificate: ${certificate.subject.replaceAll('\n', ', ')}\n  SHA-256:     ${certificateFingerprint(pem)}\n  Expires:     ${certificate.validTo}${expired}`;
}

// The menu offers only the actions that make sense for the current state.
export function setupChoices({pinned, local, missing = [], platform = process.platform}) {
 const replacesPin = pinned ? ' and replace the pinned one' : '';
 const choices = [];
 if (local) {
  const pin = !pinned ? ' and pin it' : local.fingerprint === pinned ? (missing.length ? ' (restores the missing secrets)' : '') : replacesPin;
  choices.push({label: `Upload the certificate in this folder${pin}`, value: 'local'});
 }
 choices.push({label: `${local ? 'Replace it with a new certificate' : 'Generate a new certificate'}${replacesPin}`, value: 'generate'});
 if (platform === 'darwin') choices.push({label: 'Choose a certificate from the macOS keychain', value: 'keychain'});
 choices.push({label: 'Quit', value: 'quit'});
 return choices;
}

// Walks through choosing or creating the identity, uploading it and keeping
// the pin in sync. GitHub never returns secret values, so only their presence
// is checked and overwriting them is confirmed explicitly.
export async function wizard({ask, gh = (args, input) => run('gh', args, input), openssl, platform = process.platform, dir: suggestedDir = defaultIdentityDir, pinPath = defaultPinPath}) {
 const work = fs.mkdtempSync(path.join(os.tmpdir(), 'racomi-signing-'));
 try {
  console.log('racomi macOS release signing\n');
  const dir = path.resolve(await askWithDefault(ask, 'Identity directory', suggestedDir));
  const pinned = readPin(pinPath);
  const stored = identityFiles.every((name) => fs.existsSync(path.join(dir, name))) ? readIdentityDir(dir) : undefined;
  const local = stored && {...stored, fingerprint: certificateFingerprint(stored.certificate)};
  console.log(`Pinned certificate: ${pinned ?? 'none'}`);
  console.log(`Local certificate:  ${local ? `${local.fingerprint}${local.fingerprint === pinned ? ' (pinned)' : ''}` : 'none'} in ${dir}`);
  if (local) console.log(describe(local.certificate));
  const repository = await askWithDefault(ask, 'GitHub repository', defaultRepository);
  requireThat(/^[\w.-]+\/[\w.-]+$/.test(repository), 'Invalid repository; expected OWNER/NAME.');
  gh(['auth', 'status']);
  const names = new Set(gh(['secret', 'list', '--repo', repository, '--json', 'name', '--jq', '.[].name']).split('\n').map((name) => name.trim()));
  const present = [certificateSecret, passwordSecret].filter((name) => names.has(name));
  const missing = [certificateSecret, passwordSecret].filter((name) => !names.has(name));
  console.log(`GitHub secrets:     ${missing.length ? `missing ${missing.join(', ')}` : 'both present'}`);
  if (pinned && missing.length && local?.fingerprint !== pinned) console.log('The pinned certificate is not in this folder; choose the folder holding its backup, or replace it.');
  const action = await choose(ask, 'What would you like to do?', setupChoices({pinned, local, missing, platform}));
  if (action === 'quit') return {uploaded: false};
  let replaceConfirmed = false;
  const confirmReplace = async (message) => {
   console.log(`${message} ${replaceWarning}`);
   requireThat(await confirmWord(ask, 'Replace the certificate?', 'replace'), 'Cancelled; nothing was changed.');
   replaceConfirmed = true;
  };
  let source;
  if (action === 'generate') {
   if (local || pinned) await confirmReplace(`${local ? `The current files move to a replaced-<time> folder in ${dir}. ` : ''}${pinned ? 'The new certificate replaces the pinned one.' : ''}`.trim());
   const created = generate({dir, replace: Boolean(local), openssl});
   if (created.replaced) console.log(`Moved the previous identity to ${created.replaced}.`);
   console.log(`Created a new certificate in ${dir}; back up this folder offline.`);
   source = readIdentityDir(dir);
  } else {
   source = action === 'keychain' ? await readKeychainIdentity(ask, work) : local;
  }
  const fingerprint = certificateFingerprint(source.certificate);
  if (pinned && pinned !== fingerprint && !replaceConfirmed) await confirmReplace(`This certificate replaces the pinned one (${pinned}).`);
  console.log(`Ready to upload to ${repository}:\n${describe(source.certificate)}`);
  if (present.length && pinned !== fingerprint && !replaceConfirmed) {
   console.log(`${repository} already has ${present.join(' and ')}; uploading replaces them. ${replaceWarning}`);
   requireThat(await confirmWord(ask, 'Overwrite the secrets?', 'overwrite'), 'Cancelled; nothing was uploaded.');
  } else {
   requireThat(await confirmWord(ask, `Upload ${certificateSecret} and ${passwordSecret}?`, 'yes'), 'Cancelled; nothing was uploaded.');
  }
  gh(['secret', 'set', certificateSecret, '--repo', repository], source.identity.toString('base64'));
  gh(['secret', 'set', passwordSecret, '--repo', repository], source.password);
  const pinChanged = pinned !== fingerprint;
  if (pinChanged) writePin(pinPath, fingerprint);
  return {uploaded: true, fingerprint, repository, overwritten: present, pinChanged};
 } finally {
  fs.rmSync(work, {recursive: true, force: true});
 }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
 const [action, ...args] = process.argv.slice(2);
 if (action === 'verify' && args.length === 1) {
  try {
   verify(path.resolve(args[0]));
   console.log(`Verified the pinned release signature: ${args[0]}`);
  } catch (error) {
   console.error(`macOS signing failed: ${error.message}`);
   process.exitCode = 1;
  }
 } else {
  const terminal = readline.createInterface({input: process.stdin, output: process.stdout});
  // Ctrl-D or Ctrl-C at a prompt cancels instead of surfacing an AbortError.
  const ask = (question) => terminal.question(question).catch((error) => { throw error?.code === 'ABORT_ERR' ? Error('Cancelled.') : error; });
  try {
   requireThat(action === undefined, 'Run `node scripts/macos-signing.mjs` without arguments and answer its questions; release packaging uses `verify APP_BUNDLE`.');
   requireThat(process.stdin.isTTY && process.stdout.isTTY, 'Run this script in an interactive terminal.');
   const result = await wizard({ask});
   if (result.uploaded) console.log(`${result.overwritten.length ? 'Overwrote' : 'Uploaded'} the signing secrets in ${result.repository} for certificate ${result.fingerprint}.`);
   if (result.pinChanged) console.log('Pinned it in build/macos/signing.json; commit that file before the next release.');
  } catch (error) {
   console.error(`macOS signing failed: ${error.message}`);
   process.exitCode = 1;
  } finally {
   terminal.close();
  }
 }
}
