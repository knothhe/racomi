#!/usr/bin/env node
// Creates and uploads the fixed macOS code-signing identity that release.yml
// imports. A self-signed certificate keeps the designated requirement stable
// across releases, so Keychain keeps trusting updated builds. It does not
// replace Developer ID notarization for Gatekeeper.
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import readline from 'node:readline/promises';
import {execFileSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';

export const certificateSecret = 'MACOS_SIGNING_CERTIFICATE';
export const passwordSecret = 'MACOS_SIGNING_CERTIFICATE_PASSWORD';
export const defaultIdentityDir = path.join(os.homedir(), '.config/racomi-release/macos-signing');
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

// Walks through choosing or creating the identity and uploading it. GitHub
// never returns secret values, so overwriting existing secrets is confirmed
// explicitly rather than compared.
export async function wizard({ask, gh = (args, input) => run('gh', args, input), openssl, platform = process.platform, dir: suggestedDir = defaultIdentityDir}) {
 const work = fs.mkdtempSync(path.join(os.tmpdir(), 'racomi-signing-'));
 try {
  const dir = path.resolve(await askWithDefault(ask, 'Identity directory', suggestedDir));
  const local = identityFiles.every((name) => fs.existsSync(path.join(dir, name)));
  const options = [];
  if (local) {
   console.log(`Found a signing identity in ${dir}:\n${describe(fs.readFileSync(path.join(dir, 'certificate.pem')))}`);
   options.push({label: 'Upload this certificate', value: 'local'}, {label: 'Replace it with a new certificate', value: 'replace'});
  } else {
   options.push({label: 'Generate a new certificate', value: 'generate'});
  }
  if (platform === 'darwin') options.push({label: 'Choose a certificate from the macOS keychain', value: 'keychain'});
  options.push({label: 'Quit', value: 'quit'});
  const action = await choose(ask, 'What would you like to do?', options);
  if (action === 'quit') return {uploaded: false};
  if (action === 'replace') {
   console.log(`The current files move to a replaced-<time> folder in ${dir}. ${replaceWarning}`);
   requireThat(await confirmWord(ask, 'Replace the certificate?', 'replace'), 'Cancelled; the certificate was kept.');
  }
  if (action === 'generate' || action === 'replace') {
   const created = generate({dir, replace: action === 'replace', openssl});
   if (created.replaced) console.log(`Moved the previous identity to ${created.replaced}.`);
   console.log(`Created a new certificate in ${dir}; back up this folder offline.`);
  }
  const source = action === 'keychain' ? await readKeychainIdentity(ask, work) : readIdentityDir(dir);
  const fingerprint = certificateFingerprint(source.certificate);
  const repository = await askWithDefault(ask, 'GitHub repository', defaultRepository);
  gh(['auth', 'status']);
  const names = new Set(gh(['secret', 'list', '--repo', repository, '--json', 'name', '--jq', '.[].name']).split('\n').map((name) => name.trim()));
  const existing = [certificateSecret, passwordSecret].filter((name) => names.has(name));
  console.log(`Ready to upload to ${repository}:\n${describe(source.certificate)}`);
  if (existing.length) {
   console.log(`${repository} already has ${existing.join(' and ')}; uploading replaces them. ${replaceWarning}`);
   requireThat(await confirmWord(ask, 'Overwrite the secrets?', 'overwrite'), 'Cancelled; nothing was uploaded.');
  } else {
   requireThat(await confirmWord(ask, `Upload ${certificateSecret} and ${passwordSecret}?`, 'yes'), 'Cancelled; nothing was uploaded.');
  }
  gh(['secret', 'set', certificateSecret, '--repo', repository], source.identity.toString('base64'));
  gh(['secret', 'set', passwordSecret, '--repo', repository], source.password);
  return {uploaded: true, fingerprint, repository, overwritten: existing};
 } finally {
  fs.rmSync(work, {recursive: true, force: true});
 }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
 const prompt = readline.createInterface({input: process.stdin, output: process.stdout});
 try {
  requireThat(process.argv.length === 2, 'This script takes no arguments; run it without options and answer its questions.');
  requireThat(process.stdin.isTTY && process.stdout.isTTY, 'Run this script in an interactive terminal.');
  const result = await wizard({ask: (question) => prompt.question(question)});
  if (result.uploaded) console.log(`${result.overwritten.length ? 'Overwrote' : 'Uploaded'} the signing secrets in ${result.repository} for certificate ${result.fingerprint}.`);
 } catch (error) {
  console.error(`macOS signing failed: ${error.message}`);
  process.exitCode = 1;
 } finally {
  prompt.close();
 }
}
