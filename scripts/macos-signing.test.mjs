import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import {execFileSync} from 'node:child_process';
import {certificateFingerprint, certificateSecret, generate, parseIdentityList, parseOptions, passwordSecret, selectIdentity, upload} from './macos-signing.mjs';

const temporary = () => fs.mkdtempSync(path.join(os.tmpdir(), 'racomi-macos-signing-test-'));

// Records gh calls; `secrets` lists the names the repository already has.
const fakeGh = (secrets) => {
 const calls = [];
 const gh = (args, input) => {
  calls.push({args, input});
  return args[1] === 'list' ? secrets.join('\n') : '';
 };
 return {gh, calls, sets: () => calls.filter((call) => call.args[1] === 'set')};
};

test('generate creates a private code-signing identity and replaces it only with --force', () => {
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
  assert.throws(() => generate({dir}), /already exists.*--force/);
  const second = generate({dir, force: true});
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
  assert.throws(() => generate({dir, force: true, openssl: () => { throw Error('openssl failed'); }}), /openssl failed/);
  assert.equal(certificateFingerprint(fs.readFileSync(path.join(dir, 'certificate.pem'))), first.fingerprint);
  assert.deepEqual(fs.readdirSync(dir).sort(), ['certificate.pem', 'identity-password', 'identity.p12']);
 } finally {
  fs.rmSync(root, {recursive: true, force: true});
 }
});

test('upload sets both secrets and overwrites existing ones only with --force', async () => {
 const root = temporary();
 try {
  const dir = path.join(root, 'identity');
  const {fingerprint} = generate({dir});
  const fresh = fakeGh(['SPARKLE_PRIVATE_KEY']);
  const result = await upload({dir, gh: fresh.gh, confirm: async () => 'yes'});
  assert.equal(result.fingerprint, fingerprint);
  assert.deepEqual(result.overwritten, []);
  assert.deepEqual(fresh.sets().map((call) => call.args[2]), [certificateSecret, passwordSecret]);
  assert.deepEqual(fresh.sets()[0].input, fs.readFileSync(path.join(dir, 'identity.p12')).toString('base64'));
  assert.equal(fresh.sets()[1].input, fs.readFileSync(path.join(dir, 'identity-password'), 'utf8'));
  assert.ok(fresh.sets().every((call) => call.args.includes('knothhe/racomi')));

  const configured = fakeGh([certificateSecret, passwordSecret]);
  await assert.rejects(upload({dir, gh: configured.gh, confirm: async () => 'yes'}), /already has .*--force/);
  assert.equal(configured.sets().length, 0);
  const forced = await upload({dir, gh: configured.gh, force: true, repository: 'owner/fork', confirm: async () => 'yes'});
  assert.deepEqual(forced.overwritten, [certificateSecret, passwordSecret]);
  assert.ok(configured.sets().every((call) => call.args.includes('owner/fork')));

  const declined = fakeGh([]);
  await assert.rejects(upload({dir, gh: declined.gh, confirm: async () => 'no'}), /Cancelled/);
  assert.equal(declined.sets().length, 0);
  await assert.rejects(upload({dir: path.join(root, 'missing'), gh: fakeGh([]).gh}), /generate` first/);
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

test('options accept only known flags with values', () => {
 assert.deepEqual(parseOptions(['--repo', 'owner/name', '--force'], {repo: String, force: Boolean}), {repo: 'owner/name', force: true});
 assert.throws(() => parseOptions(['--repo'], {repo: String}), /needs a value/);
 assert.throws(() => parseOptions(['--yes'], {force: Boolean}), /Unknown option/);
});
