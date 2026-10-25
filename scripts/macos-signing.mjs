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
// mistaken --force can still be undone before the secrets are overwritten.
export function generate({dir = defaultIdentityDir, force = false, openssl = (args) => run('openssl', args)} = {}) {
 const files = identityFiles.map((name) => path.join(dir, name));
 const existing = files.filter((file) => fs.existsSync(file));
 requireThat(!existing.length || force, `A macOS signing identity already exists in ${dir}. ${replaceWarning} Pass --force to replace it.`);
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
 requireThat(identityFiles.every((name) => fs.existsSync(path.join(dir, name))), `No macOS signing identity in ${dir}; run \`node scripts/macos-signing.mjs generate\` first.`);
 return {certificate: fs.readFileSync(certificate, 'utf8'), identity: fs.readFileSync(identity), password: fs.readFileSync(password, 'utf8').trim()};
}

// Exports one code-signing identity from the maintainer's macOS keychain.
async function readKeychainIdentity(prompt, work) {
 requireThat(process.platform === 'darwin', 'Uploading a keychain identity requires macOS.');
 const keychains = run('/usr/bin/security', ['list-keychains', '-d', 'user']).split('\n').map((line) => line.trim().replace(/^"|"$/g, '')).filter(Boolean);
 const identities = keychains.flatMap((keychain) => parseIdentityList(run('/usr/bin/security', ['find-identity', '-p', 'codesigning', keychain])).map((identity) => ({...identity, keychain})));
 requireThat(identities.length, 'No code-signing identity was found in the keychain search list.');
 identities.forEach((identity, index) => console.log(`${index + 1}) ${identity.name}${identity.status ? ` (${identity.status})` : ''}\n   ${identity.hash}  ${identity.keychain}`));
 const choice = Number(await prompt.question('Select a signing certificate: '));
 const selected = identities[choice - 1];
 requireThat(Number.isInteger(choice) && selected, 'No signing certificate selected.');
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

// GitHub never returns secret values, so an existing secret cannot be compared
// with the new identity; overwriting it requires --force.
export async function upload({repository = defaultRepository, dir = defaultIdentityDir, keychain = false, force = false,
 gh = (args, input) => run('gh', args, input), confirm} = {}) {
 gh(['auth', 'status']);
 const prompt = readline.createInterface({input: process.stdin, output: process.stdout});
 const work = fs.mkdtempSync(path.join(os.tmpdir(), 'racomi-signing-upload-'));
 try {
  const source = keychain ? await readKeychainIdentity(prompt, work) : readIdentityDir(dir);
  const fingerprint = certificateFingerprint(source.certificate);
  const certificate = new crypto.X509Certificate(source.certificate);
  if (Date.parse(certificate.validTo) < Date.now()) console.log(`Warning: this certificate expired on ${certificate.validTo}.`);
  const names = new Set(gh(['secret', 'list', '--repo', repository, '--json', 'name', '--jq', '.[].name']).split('\n').map((name) => name.trim()));
  const existing = [certificateSecret, passwordSecret].filter((name) => names.has(name));
  requireThat(!existing.length || force, `${repository} already has ${existing.join(' and ')}. ${replaceWarning} Pass --force to overwrite.`);
  console.log(`Certificate: ${certificate.subject.replaceAll('\n', ', ')}\nSHA-256:     ${fingerprint}\nRepository:  ${repository}`);
  if (existing.length) console.log(`This overwrites ${existing.join(' and ')}. ${replaceWarning}`);
  const ask = confirm ?? (process.stdin.isTTY ? (question) => prompt.question(question) : undefined);
  if (ask) requireThat((await ask(`Upload ${certificateSecret} and ${passwordSecret}? Type yes to continue: `)).trim() === 'yes', 'Cancelled; nothing was uploaded.');
  gh(['secret', 'set', certificateSecret, '--repo', repository], source.identity.toString('base64'));
  gh(['secret', 'set', passwordSecret, '--repo', repository], source.password);
  return {fingerprint, overwritten: existing};
 } finally {
  prompt.close();
  fs.rmSync(work, {recursive: true, force: true});
 }
}

export function parseOptions(args, allowed) {
 const options = {};
 for (let index = 0; index < args.length; index++) {
  const name = args[index].replace(/^--/, '');
  requireThat(args[index].startsWith('--') && name in allowed, `Unknown option: ${args[index]}`);
  if (allowed[name] === Boolean) options[name] = true;
  else {
   requireThat(index + 1 < args.length, `${args[index]} needs a value`);
   options[name] = args[++index];
  }
 }
 return options;
}

const usage = 'Usage: macos-signing.mjs generate [--dir DIR] [--force] | upload [--repo OWNER/NAME] [--dir DIR | --keychain] [--force]';

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
 try {
  const [action, ...args] = process.argv.slice(2);
  if (action === 'generate') {
   const options = parseOptions(args, {dir: String, force: Boolean});
   const result = generate({...options, dir: options.dir ? path.resolve(options.dir) : undefined});
   if (result.replaced) console.log(`Moved the previous identity to ${result.replaced}.`);
   console.log(`Created certificate SHA-256 ${result.fingerprint} in ${result.dir}; back it up offline.`);
   console.log('Upload it with: node scripts/macos-signing.mjs upload');
  } else if (action === 'upload') {
   const options = parseOptions(args, {repo: String, dir: String, keychain: Boolean, force: Boolean});
   requireThat(!(options.dir && options.keychain), '--dir and --keychain cannot be combined');
   const result = await upload({repository: options.repo, dir: options.dir ? path.resolve(options.dir) : undefined, keychain: options.keychain, force: options.force});
   console.log(`${result.overwritten.length ? 'Overwrote' : 'Uploaded'} the signing secrets for certificate ${result.fingerprint}.`);
  } else {
   throw Error(usage);
  }
 } catch (error) {
  console.error(`macOS signing failed: ${error.message}`);
  process.exitCode = 1;
 }
}
