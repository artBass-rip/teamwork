import {createCipheriv,createDecipheriv,randomBytes} from 'node:crypto';
import {existsSync,mkdirSync,readFileSync,writeFileSync,rmSync} from 'node:fs';
import {dirname} from 'node:path';

export class LlmSecretStore {
  constructor(directory) { this.directory=directory; mkdirSync(directory,{recursive:true}); this.keyPath=`${directory}/key`; this.valuePath=`${directory}/external-key.enc`; }
  key() { if(!existsSync(this.keyPath)) writeFileSync(this.keyPath,randomBytes(32),{mode:0o600}); return readFileSync(this.keyPath); }
  save(value) { const iv=randomBytes(12),cipher=createCipheriv('aes-256-gcm',this.key(),iv),data=Buffer.concat([cipher.update(String(value),'utf8'),cipher.final()]); writeFileSync(this.valuePath,JSON.stringify({iv:iv.toString('base64'),tag:cipher.getAuthTag().toString('base64'),data:data.toString('base64')}),{mode:0o600}); }
  load() { if(!existsSync(this.valuePath)) return ''; const value=JSON.parse(readFileSync(this.valuePath,'utf8')); const decipher=createDecipheriv('aes-256-gcm',this.key(),Buffer.from(value.iv,'base64')); decipher.setAuthTag(Buffer.from(value.tag,'base64')); return Buffer.concat([decipher.update(Buffer.from(value.data,'base64')),decipher.final()]).toString('utf8'); }
  clear() { rmSync(this.valuePath,{force:true}); }
  configured() { return existsSync(this.valuePath); }
}
