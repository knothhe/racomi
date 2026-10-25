import assert from 'node:assert/strict';
import test from 'node:test';
import {generateKeyPairSync,sign} from 'node:crypto';
import {verifyAppcast} from './verify-appcast.mjs';
import {updateFeed,verifyUpdateFeed} from './update-feed.mjs';

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

test('update feed pins Linux and Windows archives to the embedded public key',()=>{
 const {privateKey,publicKey}=generateKeyPairSync('ed25519');
 const key=publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('base64');
 const linux=Buffer.from('isolated Linux update archive fixture');
 const windows=Buffer.from('isolated Windows ZIP fixture');
 const commit='0123456789abcdef0123456789abcdef01234567';
 const signed=(bytes)=>({bytes,signature:sign(null,bytes,privateKey).toString('base64')});
 const archives={'linux-amd64':linux,'windows-amd64':windows};
 const feed=updateFeed('0.2.0',commit,{'linux-amd64':signed(linux),'windows-amd64':signed(windows)});
 assert.equal(feed.assets['linux-amd64'].url,'https://github.com/knothhe/racomi/releases/download/v0.2.0/racomi-0.2.0-x86_64-linux.tar.gz');
 assert.equal(feed.assets['windows-amd64'].url,'https://github.com/knothhe/racomi/releases/download/v0.2.0/racomi-0.2.0-x86_64-windows.zip');
 verifyUpdateFeed(feed,archives,'0.2.0',commit,key);
 const corrupt=Buffer.from(windows);corrupt[0]^=1;
 assert.throws(()=>verifyUpdateFeed(feed,{...archives,'windows-amd64':corrupt},'0.2.0',commit,key),/identity/);
 const resigned=updateFeed('0.2.0',commit,{'linux-amd64':signed(linux),'windows-amd64':{bytes:corrupt,signature:feed.assets['windows-amd64'].edSignature}});
 assert.throws(()=>verifyUpdateFeed(resigned,{...archives,'windows-amd64':corrupt},'0.2.0',commit,key),/signature/);
 assert.throws(()=>verifyUpdateFeed(feed,archives,'0.2.1',commit,key),/version/);
 assert.throws(()=>verifyUpdateFeed(feed,archives,'0.2.0','main',key),/version/);
 assert.throws(()=>verifyUpdateFeed(feed,{'linux-amd64':linux},'0.2.0',commit,key),/platform/);
 const swapped=structuredClone(feed);swapped.assets['windows-amd64'].url=feed.assets['linux-amd64'].url;
 assert.throws(()=>verifyUpdateFeed(swapped,archives,'0.2.0',commit,key),/identity/);
 const other=generateKeyPairSync('ed25519').publicKey.export({format:'der',type:'spki'}).subarray(-32).toString('base64');
 assert.throws(()=>verifyUpdateFeed(feed,archives,'0.2.0',commit,other),/signature/);
});
