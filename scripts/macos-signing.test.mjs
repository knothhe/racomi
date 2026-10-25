import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import test from 'node:test';
import {execFileSync} from 'node:child_process';
import {assertCertificateRequirement, certificateFingerprint, certificateSecret, generate, parseIdentityList, passwordSecret, readPin, selectIdentity, setupChoices, verify, wizard} from './macos-signing.mjs';

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

const pinOf = (file) => JSON.parse(fs.readFileSync(file, 'utf8')).certificateSHA256;
const writePinFile = (file, fingerprint) => fs.writeFileSync(file, JSON.stringify({schema: 1, certificateSHA256: fingerprint}));

test('wizard generates, uploads and pins a certificate after confirmation', async () => {
 const root = temporary();
 try {
  const dir = path.join(root, 'identity'), pinPath = path.join(root, 'signing.json');
  const github = fakeGh(['SPARKLE_PRIVATE_KEY']);
  const script = answers(dir, '', '1', 'yes');
  const result = await wizard({ask: script.ask, gh: github.gh, platform: 'linux', pinPath});
  assert.equal(script.left(), 0);
  assert.equal(result.fingerprint, certificateFingerprint(fs.readFileSync(path.join(dir, 'certificate.pem'))));
  assert.deepEqual([result.repository, result.overwritten, result.pinChanged], ['knothhe/racomi', [], true]);
  assert.equal(pinOf(pinPath), result.fingerprint);
  assert.deepEqual(github.sets().map((call) => call.args[2]), [certificateSecret, passwordSecret]);
  assert.equal(github.sets()[0].input, fs.readFileSync(path.join(dir, 'identity.p12')).toString('base64'));
  assert.equal(github.sets()[1].input, fs.readFileSync(path.join(dir, 'identity-password'), 'utf8'));
 } finally {
  fs.rmSync(root, {recursive: true, force: true});
 }
});

test('wizard re-uploads the pinned certificate without overwrite or replace prompts', async () => {
 const root = temporary();
 try {
  const dir = path.join(root, 'identity'), pinPath = path.join(root, 'signing.json');
  const {fingerprint} = generate({dir});
  writePinFile(pinPath, fingerprint);
  const github = fakeGh([certificateSecret]);
  const script = answers(dir, '', '1', 'yes');
  const result = await wizard({ask: script.ask, gh: github.gh, platform: 'linux', pinPath});
  assert.equal(result.pinChanged, false);
  assert.equal(github.sets().length, 2);
  assert.ok(!script.asked.some((question) => /Type (overwrite|replace)/.test(question)));
 } finally {
  fs.rmSync(root, {recursive: true, force: true});
 }
});

test('wizard overwrites unpinned secrets only after typing overwrite', async () => {
 const root = temporary();
 try {
  const dir = path.join(root, 'identity'), pinPath = path.join(root, 'signing.json');
  generate({dir});
  const refused = fakeGh([certificateSecret, passwordSecret]);
  await assert.rejects(wizard({ask: answers(dir, '', '1', 'yes').ask, gh: refused.gh, platform: 'linux', pinPath}), /Cancelled/);
  assert.equal(refused.sets().length, 0);
  assert.equal(fs.existsSync(pinPath), false);
  const github = fakeGh([certificateSecret, passwordSecret]);
  const result = await wizard({ask: answers(dir, 'owner/fork', '1', 'overwrite').ask, gh: github.gh, platform: 'linux', pinPath});
  assert.deepEqual(result.overwritten, [certificateSecret, passwordSecret]);
  assert.ok(github.sets().every((call) => call.args.includes('owner/fork')));
  assert.equal(pinOf(pinPath), result.fingerprint);
  await assert.rejects(wizard({ask: answers(dir, 'not a repository').ask, gh: fakeGh([]).gh, platform: 'linux', pinPath}), /OWNER\/NAME/);
 } finally {
  fs.rmSync(root, {recursive: true, force: true});
 }
});

test('wizard replaces the pinned certificate only after typing replace', async () => {
 const root = temporary();
 try {
  const dir = path.join(root, 'identity'), pinPath = path.join(root, 'signing.json');
  const first = generate({dir});
  const other = generate({dir: path.join(root, 'other')});
  writePinFile(pinPath, other.fingerprint);
  // Uploading a local certificate that is not the pinned one replaces the pin.
  const declined = fakeGh([certificateSecret, passwordSecret]);
  await assert.rejects(wizard({ask: answers(dir, '', '1', 'no').ask, gh: declined.gh, platform: 'linux', pinPath}), /nothing was changed/);
  assert.equal(declined.sets().length, 0);
  assert.equal(pinOf(pinPath), other.fingerprint);
  const github = fakeGh([certificateSecret, passwordSecret]);
  const script = answers(dir, '', '1', 'replace', 'yes');
  const result = await wizard({ask: script.ask, gh: github.gh, platform: 'linux', pinPath});
  assert.equal(result.fingerprint, first.fingerprint);
  assert.equal(pinOf(pinPath), first.fingerprint);
  assert.ok(!script.asked.some((question) => /Type overwrite/.test(question)));
  // Generating replaces both the local files and the pin after one confirmation.
  const generated = await wizard({ask: answers(dir, '', '2', 'replace', 'yes').ask, gh: fakeGh([]).gh, platform: 'linux', pinPath});
  assert.notEqual(generated.fingerprint, first.fingerprint);
  assert.equal(pinOf(pinPath), generated.fingerprint);
  const replaced = fs.readdirSync(dir).find((name) => name.startsWith('replaced-'));
  assert.equal(certificateFingerprint(fs.readFileSync(path.join(dir, replaced, 'certificate.pem'))), first.fingerprint);
 } finally {
  fs.rmSync(root, {recursive: true, force: true});
 }
});

test('menu choices follow the pin, local identity, secrets and platform', async () => {
 const local = {fingerprint: 'a'.repeat(64)};
 const labels = (state) => setupChoices(state).map((choice) => choice.label);
 assert.deepEqual(labels({platform: 'linux'}), ['Generate a new certificate', 'Quit']);
 assert.deepEqual(labels({local, platform: 'linux'}), ['Upload the certificate in this folder and pin it', 'Replace it with a new certificate', 'Quit']);
 assert.deepEqual(labels({pinned: local.fingerprint, local, missing: [passwordSecret], platform: 'linux'})[0], 'Upload the certificate in this folder (restores the missing secrets)');
 assert.deepEqual(labels({pinned: 'b'.repeat(64), local, platform: 'darwin'}), ['Upload the certificate in this folder and replace the pinned one',
  'Replace it with a new certificate and replace the pinned one', 'Choose a certificate from the macOS keychain', 'Quit']);
 const root = temporary();
 try {
  const dir = path.join(root, 'identity');
  generate({dir});
  const github = fakeGh([]);
  const pinPath = path.join(root, 'signing.json');
  assert.deepEqual(await wizard({ask: answers(dir, '', '9', 'x', '3').ask, gh: github.gh, platform: 'linux', pinPath}), {uploaded: false});
  assert.deepEqual(await wizard({ask: answers(dir, '', '4').ask, gh: github.gh, platform: 'darwin', pinPath}), {uploaded: false});
  await assert.rejects(wizard({ask: answers(dir, '', '0', '', 'five').ask, gh: github.gh, platform: 'linux', pinPath}), /No option was chosen/);
  assert.equal(github.sets().length, 0);
  assert.equal(fs.existsSync(pinPath), false);
 } finally {
  fs.rmSync(root, {recursive: true, force: true});
 }
});

test('verify accepts only bundles signed with the pinned certificate', () => {
 const root = temporary();
 try {
  const pinned = generate({dir: path.join(root, 'pinned')}), other = generate({dir: path.join(root, 'other')});
  const der = (name) => new crypto.X509Certificate(fs.readFileSync(path.join(root, name, 'certificate.pem'))).raw;
  const bundle = path.join(root, 'racomi.app');
  fs.mkdirSync(path.join(bundle, 'Contents/MacOS'), {recursive: true});
  fs.writeFileSync(path.join(bundle, 'Contents/MacOS/racomi'), '');
  const requirement = 'designated => identifier "com.knothhe.racomi" and certificate leaf = H"0123456789abcdef0123456789abcdef01234567"';
  // Stands in for codesign: writes the signing certificate and reports a requirement.
  const codesign = (certificate, text = requirement) => (target, ...args) => {
   const extract = args.find((arg) => arg.startsWith('--extract-certificates='));
   if (extract) fs.writeFileSync(`${extract.split('=')[1]}0`, certificate);
   return text;
  };
  verify(bundle, {pin: pinned.fingerprint, codesign: codesign(der('pinned'))});
  assert.throws(() => verify(bundle, {pin: pinned.fingerprint, codesign: codesign(der('other'))}), /not signed with the pinned/);
  assert.throws(() => verify(bundle, {pin: pinned.fingerprint, codesign: codesign(der('pinned'), 'designated => cdhash H"0123"')}), /certificate-based/);
  assert.throws(() => verify(bundle, {pin: undefined, codesign: codesign(der('pinned'))}), /No pinned/);
  assert.notEqual(other.fingerprint, pinned.fingerprint);
  assertCertificateRequirement('designated => identifier "x" and certificate root = H"0123456789abcdef0123456789abcdef01234567"');
 } finally {
  fs.rmSync(root, {recursive: true, force: true});
 }
});

test('pin files contain only the schema and fingerprint', () => {
 const root = temporary();
 try {
  const file = path.join(root, 'signing.json');
  assert.equal(readPin(file), undefined);
  writePinFile(file, 'c'.repeat(64));
  assert.equal(readPin(file), 'c'.repeat(64));
  fs.writeFileSync(file, JSON.stringify({schema: 1, certificateSHA256: 'c'.repeat(64), note: 'extra'}));
  assert.throws(() => readPin(file), /only schema 1/);
  fs.writeFileSync(file, JSON.stringify({schema: 1, certificateSHA256: 'C'.repeat(64)}));
  assert.throws(() => readPin(file), /only schema 1/);
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
