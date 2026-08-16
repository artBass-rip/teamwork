import {existsSync, mkdirSync, readFileSync, renameSync, writeFileSync} from 'node:fs';
import {dirname} from 'node:path';

export function normalizeLabels(values) {
  if (!Array.isArray(values)) throw new Error('Метки должны быть массивом');
  const result = [];
  const seen = new Set();
  for (const value of values) {
    const label = String(value).trim().replace(/\s+/g, ' ');
    if (!label) continue;
    if (label.length > 40) throw new Error('Метка не должна превышать 40 символов');
    if (!/^[\p{L}\p{N}][\p{L}\p{N} _./-]*$/u.test(label)) throw new Error(`Недопустимая метка: ${label}`);
    const key = label.toLocaleLowerCase();
    if (!seen.has(key)) { seen.add(key); result.push(label); }
  }
  if (result.length > 10) throw new Error('Для задачи разрешено не более 10 меток');
  return result;
}

export class LabelStore {
  constructor(path, logger) {
    this.path = path;
    this.logger = logger;
    mkdirSync(dirname(path), {recursive: true});
    if (!existsSync(path)) this.write({});
  }

  read() {
    try {
      const value = JSON.parse(readFileSync(this.path, 'utf8'));
      return value && typeof value === 'object' && !Array.isArray(value) ? value : {};
    } catch (error) {
      this.logger.error('labels.read_failed', 'Не удалось прочитать хранилище меток', {error: error.message});
      throw new Error('Хранилище меток повреждено');
    }
  }

  write(value) {
    const temporary = `${this.path}.tmp`;
    writeFileSync(temporary, `${JSON.stringify(value, null, 2)}\n`, {mode: 0o600});
    renameSync(temporary, this.path);
  }

  all() { return this.read(); }
  list(issueKey) { return this.read()[issueKey] || []; }

  catalog() {
    const counts = new Map();
    for (const values of Object.values(this.read())) {
      for (const label of values) counts.set(label, (counts.get(label) || 0) + 1);
    }
    return [...counts].map(([name, issues]) => ({name, issues})).sort((a, b) => a.name.localeCompare(b.name, 'ru'));
  }

  set(issueKey, values) {
    const labels = normalizeLabels(values);
    const store = this.read();
    if (labels.length) store[issueKey] = labels;
    else delete store[issueKey];
    this.write(store);
    return labels;
  }

  removeEverywhere(value) {
    const [label] = normalizeLabels([value]);
    if (!label) throw new Error('Метка не указана');
    const store = this.read();
    let issues = 0;
    for (const [issueKey, values] of Object.entries(store)) {
      const filtered = values.filter(item => item.toLocaleLowerCase() !== label.toLocaleLowerCase());
      if (filtered.length === values.length) continue;
      issues += 1;
      if (filtered.length) store[issueKey] = filtered;
      else delete store[issueKey];
    }
    if (issues) this.write(store);
    return {label, issues};
  }

  prune(issueKeys) {
    const active = new Set(issueKeys);
    const store = this.read();
    const removed = Object.keys(store).filter(key => !active.has(key));
    for (const key of removed) delete store[key];
    if (removed.length) this.write(store);
    return removed;
  }
}
