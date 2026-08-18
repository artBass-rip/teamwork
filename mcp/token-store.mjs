import {createCipheriv, createDecipheriv, createHash, randomBytes} from 'node:crypto';
import {existsSync, mkdirSync, readFileSync, renameSync, rmSync, writeFileSync} from 'node:fs';
import {dirname} from 'node:path';

export class EncryptedTokenStore {
  constructor(path, secret) {
    if (!secret || secret.length < 32) throw new Error('JIRA_TOKEN_ENCRYPTION_KEY must contain at least 32 characters');
    this.path = path;
    this.key = createHash('sha256').update(secret).digest();
    mkdirSync(dirname(path), {recursive: true});
  }

  exists() { return existsSync(this.path); }

  load() {
    if (!this.exists()) return null;
    try {
      const envelope = JSON.parse(readFileSync(this.path, 'utf8'));
      const decipher = createDecipheriv('aes-256-gcm', this.key, Buffer.from(envelope.iv, 'base64'));
      decipher.setAuthTag(Buffer.from(envelope.tag, 'base64'));
      const value = Buffer.concat([decipher.update(Buffer.from(envelope.data, 'base64')), decipher.final()]);
      return JSON.parse(value.toString('utf8'));
    } catch (error) {
      const backup = `${this.path}.unreadable-${Date.now()}`;
      renameSync(this.path, backup);
      console.error(`Encrypted OAuth session could not be read and was quarantined at ${backup}: ${error.message}`);
      return null;
    }
  }

  save(value) {
    const iv = randomBytes(12);
    const cipher = createCipheriv('aes-256-gcm', this.key, iv);
    const encrypted = Buffer.concat([cipher.update(JSON.stringify(value), 'utf8'), cipher.final()]);
    const envelope = {version: 1, algorithm: 'aes-256-gcm', iv: iv.toString('base64'), tag: cipher.getAuthTag().toString('base64'), data: encrypted.toString('base64')};
    const temporary = `${this.path}.tmp`;
    writeFileSync(temporary, `${JSON.stringify(envelope)}\n`, {mode: 0o600});
    renameSync(temporary, this.path);
  }

  clear() { rmSync(this.path, {force: true}); }
}

export function persistentEncryptionSecret(path, migrationSecret = '') {
  mkdirSync(dirname(path), {recursive: true});
  if (existsSync(path)) return readFileSync(path, 'utf8').trim();
  const secret = migrationSecret.length >= 32 ? migrationSecret : randomBytes(32).toString('hex');
  const temporary = `${path}.tmp`;
  writeFileSync(temporary, `${secret}\n`, {mode: 0o600});
  renameSync(temporary, path);
  return secret;
}
