import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import {fileURLToPath} from 'node:url';

export const linuxArches = {amd64: 'x86_64'};

export function linuxArchiveName(version, goarch) { return `racomi-${version}-${linuxArches[goarch]}-linux.tar.gz`; }

// The client pins the same URL, version and signature rules; keep them in sync
// with parseLinuxUpdateFeed in internal/desktop/updatefeed.go.
export function linuxFeed(version, commit, archives) {
 const assets = {};
 for (const [goarch, {bytes, signature}] of Object.entries(archives)) {
  assets[goarch] = {url: `https://github.com/knothhe/racomi/releases/download/v${version}/${linuxArchiveName(version, goarch)}`, length: bytes.length, sha256: crypto.createHash('sha256').update(bytes).digest('hex'), edSignature: signature};
 }
 return {version, commit, releaseURL: `https://github.com/knothhe/racomi/releases/tag/v${version}`, assets};
}

export function verifyLinuxFeed(feed, archives, version, commit, publicKey) {
 if (feed.version !== version || feed.commit !== commit || !/^[0-9a-f]{40}$/.test(commit)) throw Error('Linux feed version mismatch');
 const rawKey = Buffer.from(publicKey, 'base64');
 if (rawKey.length !== 32) throw Error('Invalid update public key');
 const key = crypto.createPublicKey({key: Buffer.concat([Buffer.from('302a300506032b6570032100', 'hex'), rawKey]), format: 'der', type: 'spki'});
 if (Object.keys(feed.assets || {}).sort().join() !== Object.keys(archives).sort().join()) throw Error('Linux feed architecture mismatch');
 for (const [goarch, bytes] of Object.entries(archives)) {
  const asset = feed.assets[goarch];
  const expected = `https://github.com/knothhe/racomi/releases/download/v${version}/${linuxArchiveName(version, goarch)}`;
  if (asset.url !== expected || asset.length !== bytes.length || asset.sha256 !== crypto.createHash('sha256').update(bytes).digest('hex')) throw Error('Linux feed archive identity/length mismatch');
  if (!crypto.verify(null, bytes, key, Buffer.from(asset.edSignature || '', 'base64'))) throw Error('Linux update archive signature does not match the embedded public key');
 }
}

// usage: write|verify FEED VERSION COMMIT ARCHIVE_DIR [SIGNATURE_FILE]
// write reads the Ed25519 signature that Sparkle's sign_update printed.
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
 const [action, feedPath, version, commit, directory, signatureFile] = process.argv.slice(2);
 const config = JSON.parse(fs.readFileSync(new URL('../build/updates.json', import.meta.url)));
 const bytes = Object.fromEntries(Object.keys(linuxArches).map((goarch) => [goarch, fs.readFileSync(path.join(directory, linuxArchiveName(version, goarch)))]));
 if (action === 'write') {
  const output = fs.readFileSync(signatureFile, 'utf8');
  const signature = output.match(/sparkle:edSignature="([^"]+)"/)?.[1] || output.trim();
  const feed = linuxFeed(version, commit, {amd64: {bytes: bytes.amd64, signature}});
  verifyLinuxFeed(feed, bytes, version, commit, config.publicKey);
  fs.writeFileSync(feedPath, `${JSON.stringify(feed, null, 2)}\n`);
 } else if (action === 'verify') {
  verifyLinuxFeed(JSON.parse(fs.readFileSync(feedPath, 'utf8')), bytes, version, commit, config.publicKey);
 } else {
  throw Error('usage: verify-linux-feed.mjs write|verify FEED VERSION COMMIT ARCHIVE_DIR [SIGNATURE_FILE]');
 }
 console.log('Linux feed archive identity, version, length and Ed25519 signature verified.');
}
