import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import {execFileSync} from 'node:child_process';
import {certificateFingerprint, certificateSecret, generate, parseIdentityList, passwordSecret, selectIdentity, wizard} from './macos-signing.mjs';

const temporary = () => fs.mkdtempSync(path.join(os.tmpdir(), 'racomi-macos-signing-test-'));

// Records gh calls; `secrets` lists the names the repository already has.
// Answers the wizard's questions in order and records what it asked.
const answers = (...replies) => {
 const asked = [];
 const ask = async (question) => {
  asked.push(question);
  if (!replies.length) throw Error(`unexpected question: ${question}`);
  return replies.shift();
 };
 return {ask, asked, left: () => replies.length};
};

const fakeGh = (secrets) => {
 const calls = [];
 const gh = (args, input) => {
  calls.push({args, input});
  return args[1] === 'list' ? secrets.join('\n') : '';
 };
 return {gh, calls, sets: () => calls.filter((call) => call.args[1] === 'set')};
};

test('generate creates a private code-signing identity and replaces it only on request', () => {
 const root = temporary();
 try {
  const dir = path.join(root, 'identity dir');
  const first = generate({dir});
  const certificate = new crypto.X509Certificate(fs.readFileSync(path.join(dir, 'certificate.pem')));
  assert.ok(certificate.keyUsage.includes('1.3.6.1.5.5.7.3.3'));
  assert.equal(certificate.ca, false);
  assert.equal(certificateFingerprint(fs.readFileSync(path.join(dir, 'certificate.pem'))), first.fingerprint);
  assert.ok(fs.statSync(path.join(dir, 'identity.p12')).size > 0);
  if (process.platform !== 'win32') {
   assert.equal(fs.statSync(dir).mode & 0o777, 0o700);
   for (const name of ['certificate.pem', 'identity.p12', 'identity-password']) assert.equal(fs.statSync(path.join(dir, name)).mode & 0o777, 0o600);
  }
  assert.throws(() => generate({dir}), /already exists/);
  const second = generate({dir, replace: true});
  assert.notEqual(second.fingerprint, first.fingerprint);
  assert.equal(certificateFingerprint(fs.readFileSync(path.join(second.replaced, 'certificate.pem'))), first.fingerprint);
  assert.deepEqual(fs.readdirSync(dir).filter((name) => name.startsWith('.generate-')), []);
 } finally {
  fs.rmSync(root, {recursive: true, force: true});
 }
});

test('a failed generate leaves the existing identity untouched', () => {
 const root = temporary();
 try {
  const dir = path.join(root, 'identity');
  const first = generate({dir});
  assert.throws(() => generate({dir, replace: true, openssl: () => { throw Error('openssl failed'); }}), /openssl failed/);
  assert.equal(certificateFingerprint(fs.readFileSync(path.join(dir, 'certificate.pem'))), first.fingerprint);
  assert.deepEqual(fs.readdirSync(dir).sort(), ['certificate.pem', 'identity-password', 'identity.p12']);
 } finally {
  fs.rmSync(root, {recursive: true, force: true});
 }
});

test('wizard generates a certificate and uploads it after confirmation', async () => {
 const root = temporary();
 try {
  const dir = path.join(root, 'identity');
  const github = fakeGh(['SPARKLE_PRIVATE_KEY']);
  const script = answers(dir, '1', '', 'yes');
  const result = await wizard({ask: script.ask, gh: github.gh, platform: 'linux'});
  assert.equal(script.left(), 0);
  assert.equal(result.fingerprint, certificateFingerprint(fs.readFileSync(path.join(dir, 'certificate.pem'))));
  assert.deepEqual(result.overwritten, []);
  assert.equal(result.repository, 'knothhe/racomi');
  assert.deepEqual(github.sets().map((call) => call.args[2]), [certificateSecret, passwordSecret]);
  assert.equal(github.sets()[0].input, fs.readFileSync(path.join(dir, 'identity.p12')).toString('base64'));
  assert.equal(github.sets()[1].input, fs.readFileSync(path.join(dir, 'identity-password'), 'utf8'));
 } finally {
  fs.rmSync(root, {recursive: true, force: true});
 }
});

test('wizard overwrites existing secrets only after typing overwrite', async () => {
 const root = temporary();
 try {
  const dir = path.join(root, 'identity');
  const {fingerprint} = generate({dir});
  const refused = fakeGh([certificateSecret, passwordSecret]);
  await assert.rejects(wizard({ask: answers(dir, '1', '', 'yes').ask, gh: refused.gh, platform: 'linux'}), /Cancelled/);
  assert.equal(refused.sets().length, 0);
  const github = fakeGh([certificateSecret, passwordSecret]);
  const script = answers(dir, '1', 'owner/fork', 'overwrite');
  const result = await wizard({ask: script.ask, gh: github.gh, platform: 'linux'});
  assert.equal(result.fingerprint, fingerprint);
  assert.deepEqual(result.overwritten, [certificateSecret, passwordSecret]);
  assert.ok(github.sets().every((call) => call.args.includes('owner/fork')));
  assert.ok(script.asked.some((question) => /Type overwrite/.test(question)));
 } finally {
  fs.rmSync(root, {recursive: true, force: true});
 }
});

test('wizard replaces a local certificate only after typing replace', async () => {
 const root = temporary();
 try {
  const dir = path.join(root, 'identity');
  const first = generate({dir});
  await assert.rejects(wizard({ask: answers(dir, '2', 'no').ask, gh: fakeGh([]).gh, platform: 'linux'}), /certificate was kept/);
  assert.equal(certificateFingerprint(fs.readFileSync(path.join(dir, 'certificate.pem'))), first.fingerprint);
  const github = fakeGh([]);
  const result = await wizard({ask: answers(dir, '2', 'replace', '', 'yes').ask, gh: github.gh, platform: 'linux'});
  assert.notEqual(result.fingerprint, first.fingerprint);
  const replaced = fs.readdirSync(dir).find((name) => name.startsWith('replaced-'));
  assert.equal(certificateFingerprint(fs.readFileSync(path.join(dir, replaced, 'certificate.pem'))), first.fingerprint);
  assert.equal(github.sets().length, 2);
 } finally {
  fs.rmSync(root, {recursive: true, force: true});
 }
});

test('wizard offers the keychain only on macOS, re-asks invalid choices and can quit', async () => {
 const root = temporary();
 try {
  const dir = path.join(root, 'identity');
  generate({dir});
  // Linux lists upload, replace and quit; option 3 quits without touching GitHub.
  const linux = fakeGh([]);
  assert.deepEqual(await wizard({ask: answers(dir, '9', 'x', '3').ask, gh: linux.gh, platform: 'linux'}), {uploaded: false});
  assert.equal(linux.calls.length, 0);
  assert.deepEqual(await wizard({ask: answers(dir, '4').ask, gh: linux.gh, platform: 'darwin'}), {uploaded: false});
  await assert.rejects(wizard({ask: answers(dir, '0', '', 'five').ask, gh: linux.gh, platform: 'linux'}), /No option was chosen/);
 } finally {
  fs.rmSync(root, {recursive: true, force: true});
 }
});

test('keychain identities are parsed once and repacked from a multi-identity export', () => {
 const output = `  1) 0123456789ABCDEF0123456789ABCDEF01234567 "racomi Release Signing (self-signed)" (CSSMERR_TP_NOT_TRUSTED)\n  2) 89ABCDEF0123456789ABCDEF0123456789ABCDEF "Apple Development: someone"\n  3) 0123456789ABCDEF0123456789ABCDEF01234567 "racomi Release Signing (self-signed)" (CSSMERR_TP_NOT_TRUSTED)\n     3 identities found\n`;
 assert.deepEqual(parseIdentityList(output).map((identity) => [identity.hash.slice(0, 4), identity.status]), [['0123', 'CSSMERR_TP_NOT_TRUSTED'], ['89AB', '']]);
 const root = temporary();
 try {
  // Stand in for `security export` of a keychain holding two identities.
  const dump = ['one', 'two'].map((name) => {
   const dir = path.join(root, name);
   generate({dir});
   return execFileSync('openssl', ['pkcs12', '-in', path.join(dir, 'identity.p12'), '-nodes', '-passin', `file:${path.join(dir, 'identity-password')}`], {encoding: 'utf8'});
  });
  const second = new crypto.X509Certificate(fs.readFileSync(path.join(root, 'two', 'certificate.pem')));
  const sha1 = second.fingerprint.replaceAll(':', '');
  const selected = selectIdentity(dump.join('\n'), sha1);
  assert.equal(new crypto.X509Certificate(selected.certificate).fingerprint256, second.fingerprint256);
  const spki = (key) => crypto.createPublicKey(key).export({type: 'spki', format: 'der'}).toString('hex');
  assert.equal(spki(selected.key), second.publicKey.export({type: 'spki', format: 'der'}).toString('hex'));
  const certificatesOnly = dump.map((pem) => pem.match(/-----BEGIN CERTIFICATE-----[\s\S]+?-----END CERTIFICATE-----/)[0]).join('\n');
  assert.throws(() => selectIdentity(certificatesOnly, sha1), /private key/);
  assert.throws(() => selectIdentity(dump[0], sha1), /certificate is missing/);
 } finally {
  fs.rmSync(root, {recursive: true, force: true});
 }
});
