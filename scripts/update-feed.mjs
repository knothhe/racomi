import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import {fileURLToPath} from 'node:url';

// One update.json serves Linux and Windows; macOS uses Sparkle's appcast.xml.
// Keys are GOOS-GOARCH; keep names and URL rules in sync with parseUpdateFeed
// in internal/desktop/updatefeed.go.
export const platforms = {
 'linux-amd64': (version) => `racomi-${version}-x86_64-linux.tar.gz`,
 'windows-amd64': (version) => `racomi-${version}-x86_64-windows.zip`,
};

const assetURL = (version, platform) => `https://github.com/knothhe/racomi/releases/download/v${version}/${platforms[platform](version)}`;
const sha256 = (bytes) => crypto.createHash('sha256').update(bytes).digest('hex');

export function updateFeed(version, commit, archives) {
 const assets = {};
 for (const [platform, {bytes, signature}] of Object.entries(archives)) {
  assets[platform] = {url: assetURL(version, platform), length: bytes.length, sha256: sha256(bytes), edSignature: signature};
 }
 return {version, commit, releaseURL: `https://github.com/knothhe/racomi/releases/tag/v${version}`, assets};
}

export function verifyUpdateFeed(feed, archives, version, commit, publicKey) {
 if (feed.version !== version || feed.commit !== commit || !/^[0-9a-f]{40}$/.test(commit)) throw Error('Update feed version mismatch');
 const rawKey = Buffer.from(publicKey, 'base64');
 if (rawKey.length !== 32) throw Error('Invalid update public key');
 const key = crypto.createPublicKey({key: Buffer.concat([Buffer.from('302a300506032b6570032100', 'hex'), rawKey]), format: 'der', type: 'spki'});
 if (Object.keys(feed.assets || {}).sort().join() !== Object.keys(archives).sort().join()) throw Error('Update feed platform mismatch');
 for (const [platform, bytes] of Object.entries(archives)) {
  const asset = feed.assets[platform];
  if (asset.url !== assetURL(version, platform) || asset.length !== bytes.length || asset.sha256 !== sha256(bytes)) throw Error(`${platform} archive identity/length mismatch`);
  if (!crypto.verify(null, bytes, key, Buffer.from(asset.edSignature || '', 'base64'))) throw Error(`${platform} archive signature does not match the embedded public key`);
 }
}

// usage: write|verify FEED VERSION COMMIT ARCHIVE_DIR [SIGNATURE_DIR]
// write reads <platform>.sig files that Sparkle's sign_update printed.
if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
 const [action, feedPath, version, commit, directory, signatureDir] = process.argv.slice(2);
 const config = JSON.parse(fs.readFileSync(new URL('../build/updates.json', import.meta.url)));
 const bytes = Object.fromEntries(Object.entries(platforms).map(([platform, name]) => [platform, fs.readFileSync(path.join(directory, name(version)))]));
 if (action === 'write') {
  const archives = Object.fromEntries(Object.entries(bytes).map(([platform, data]) => {
   const output = fs.readFileSync(path.join(signatureDir, `${platform}.sig`), 'utf8');
   return [platform, {bytes: data, signature: output.match(/sparkle:edSignature="([^"]+)"/)?.[1] || output.trim()}];
  }));
  const feed = updateFeed(version, commit, archives);
  verifyUpdateFeed(feed, bytes, version, commit, config.publicKey);
  fs.writeFileSync(feedPath, `${JSON.stringify(feed, null, 2)}\n`);
 } else if (action === 'verify') {
  verifyUpdateFeed(JSON.parse(fs.readFileSync(feedPath, 'utf8')), bytes, version, commit, config.publicKey);
 } else {
  throw Error('usage: update-feed.mjs write|verify FEED VERSION COMMIT ARCHIVE_DIR [SIGNATURE_DIR]');
 }
 console.log('Update feed archive identity, version, length and Ed25519 signatures verified.');
}
