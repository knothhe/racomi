import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {createPublicKey, verify} from 'node:crypto';

export function verifyAppcast(xml, bytes, version, number, publicKey) {
 const expectedURL=`https://github.com/knothhe/racomi/releases/download/v${version}/racomi-${version}-aarch64-macos.zip`;
 const enclosure=xml.match(/<enclosure\s[^>]*>/)?.[0] || '';
 for(const value of [`url="${expectedURL}"`, `length="${bytes.length}"`]) if(!enclosure.includes(value)) throw Error('Appcast archive identity/length mismatch');
 if(!xml.includes(`<sparkle:version>${number}</sparkle:version>`) || !xml.includes(`<sparkle:shortVersionString>${version}</sparkle:shortVersionString>`)) throw Error('Appcast version mismatch');
 if(xml.includes('<sparkle:deltas>')) throw Error('Only complete update archives are supported');
 const rawKey=Buffer.from(publicKey,'base64');
 if(rawKey.length!==32) throw Error('Invalid update public key');
 const key=createPublicKey({key:Buffer.concat([Buffer.from('302a300506032b6570032100','hex'),rawKey]),format:'der',type:'spki'});
 const signature=enclosure.match(/sparkle:edSignature="([^"]+)"/)?.[1] || '';
 if(!verify(null,bytes,key,Buffer.from(signature,'base64'))) throw Error('Update archive signature does not match the embedded public key');
}

if(process.argv[1] && path.resolve(process.argv[1])===fileURLToPath(import.meta.url)) {
 const [feed,archive,version,number]=process.argv.slice(2);
 const config=JSON.parse(fs.readFileSync(new URL('../build/updates.json',import.meta.url)));
 verifyAppcast(fs.readFileSync(feed,'utf8'),fs.readFileSync(archive),version,number,config.publicKey);
 console.log('Appcast archive identity, version, length and Ed25519 signature verified.');
}
