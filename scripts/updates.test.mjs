import assert from 'node:assert/strict';
import test from 'node:test';
import {generateKeyPairSync,sign} from 'node:crypto';
import {verifyAppcast} from './verify-appcast.mjs';
import {linuxFeed,verifyLinuxFeed} from './verify-linux-feed.mjs';
import {windowsFeed,verifyWindowsFeed} from './verify-windows-feed.mjs';

test('release gate verifies the pinned public key and rejects tampered/wrong-target artifacts',()=>{
 const {privateKey,publicKey}=generateKeyPairSync('ed25519');
 const key=publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('base64');
 const bytes=Buffer.from('isolated update archive fixture');
 const signature=sign(null,bytes,privateKey).toString('base64');
 const xml=`<item><sparkle:version>12</sparkle:version><sparkle:shortVersionString>0.2.0</sparkle:shortVersionString><enclosure url="https://github.com/knothhe/racomi/releases/download/v0.2.0/racomi-0.2.0-aarch64-macos.zip" sparkle:edSignature="${signature}" length="${bytes.length}" /></item>`;
 verifyAppcast(xml,bytes,'0.2.0','12',key);
 const corrupt=Buffer.from(bytes);corrupt[0]^=1;
 assert.throws(()=>verifyAppcast(xml,corrupt,'0.2.0','12',key),/signature/);
 assert.throws(()=>verifyAppcast(xml.replace('aarch64','x86_64'),bytes,'0.2.0','12',key),/identity/);
 assert.throws(()=>verifyAppcast(xml,bytes,'0.2.0','13',key),/version/);
 const other=generateKeyPairSync('ed25519').publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('base64');
 assert.throws(()=>verifyAppcast(xml,bytes,'0.2.0','12',other),/signature/);
});

test('Linux feed pins release archives to the embedded public key',()=>{
 const {privateKey,publicKey}=generateKeyPairSync('ed25519');
 const key=publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('base64');
 const bytes=Buffer.from('isolated Linux update archive fixture');
 const commit='0123456789abcdef0123456789abcdef01234567';
 const signature=sign(null,bytes,privateKey).toString('base64');
 const feed=linuxFeed('0.2.0',commit,{amd64:{bytes,signature}});
 assert.equal(feed.assets.amd64.url,'https://github.com/knothhe/racomi/releases/download/v0.2.0/racomi-0.2.0-x86_64-linux.tar.gz');
 verifyLinuxFeed(feed,{amd64:bytes},'0.2.0',commit,key);
 const corrupt=Buffer.from(bytes);corrupt[0]^=1;
 assert.throws(()=>verifyLinuxFeed(feed,{amd64:corrupt},'0.2.0',commit,key),/identity/);
 const resigned=linuxFeed('0.2.0',commit,{amd64:{bytes:corrupt,signature}});
 assert.throws(()=>verifyLinuxFeed(resigned,{amd64:corrupt},'0.2.0',commit,key),/signature/);
 assert.throws(()=>verifyLinuxFeed(feed,{amd64:bytes},'0.2.1',commit,key),/version/);
 assert.throws(()=>verifyLinuxFeed(feed,{amd64:bytes},'0.2.0','main',key),/version/);
 const other=generateKeyPairSync('ed25519').publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('base64');
 assert.throws(()=>verifyLinuxFeed(feed,{amd64:bytes},'0.2.0',commit,other),/signature/);
});

test('Windows feed verifies the exact ZIP and rejects other platforms, commits, hashes and keys',()=>{
 const {privateKey,publicKey}=generateKeyPairSync('ed25519');
 const key=publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('base64');
 const bytes=Buffer.from('isolated Windows ZIP fixture');
 const commit='0123456789abcdef0123456789abcdef01234567';
 const signature=sign(null,bytes,privateKey).toString('base64');
 const feed=windowsFeed('0.2.0',commit,{amd64:{bytes,signature}});
 assert.equal(feed.assets.amd64.url,'https://github.com/knothhe/racomi/releases/download/v0.2.0/racomi-0.2.0-x86_64-windows.zip');
 verifyWindowsFeed(feed,{amd64:bytes},'0.2.0',commit,key);
 const corrupt=Buffer.from(bytes);corrupt[0]^=1;
 assert.throws(()=>verifyWindowsFeed(feed,{amd64:corrupt},'0.2.0',commit,key),/identity/);
 const resigned=windowsFeed('0.2.0',commit,{amd64:{bytes:corrupt,signature}});
 assert.throws(()=>verifyWindowsFeed(resigned,{amd64:corrupt},'0.2.0',commit,key),/signature/);
 const other=generateKeyPairSync('ed25519').publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('base64');
 assert.throws(()=>verifyWindowsFeed(feed,{amd64:bytes},'0.2.0',commit,other),/signature/);
 assert.throws(()=>verifyWindowsFeed({...feed,commit:'main'},{amd64:bytes},'0.2.0',commit,key),/version/);
 const linux=structuredClone(feed);linux.assets.amd64.url=linux.assets.amd64.url.replace('windows.zip','linux.tar.gz');
 assert.throws(()=>verifyWindowsFeed(linux,{amd64:bytes},'0.2.0',commit,key),/identity/);
});
