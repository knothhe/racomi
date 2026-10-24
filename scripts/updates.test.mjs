import assert from 'node:assert/strict';
import test from 'node:test';
import {generateKeyPairSync,sign} from 'node:crypto';
import {verifyAppcast} from './verify-appcast.mjs';

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
