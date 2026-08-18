import {existsSync, mkdirSync, readFileSync, renameSync, writeFileSync} from 'node:fs';
import {dirname} from 'node:path';

const defaultLlm = {provider:'ollama',ollamaUrl:'http://ollama:11434',ollamaModel:'qwen2.5:3b',external:{baseUrl:'https://api.openai.com/v1',model:''}};
const empty = {projects: [], jiraBaseUrl: '', cloudId: '', groupings: {}, llm: defaultLlm};

export class WorkspaceStore {
  constructor(path) {
    this.path = path;
    mkdirSync(dirname(path), {recursive: true});
  }

  get() {
    if (!existsSync(this.path)) return {...empty};
    try {
      const value = JSON.parse(readFileSync(this.path, 'utf8'));
      return {projects: normalizeProjects(value.projects), jiraBaseUrl: normalizeBaseUrl(value.jiraBaseUrl), cloudId: String(value.cloudId || ''), groupings: value.groupings && typeof value.groupings === 'object' ? value.groupings : {}, llm: normalizeLlm(value.llm)};
    } catch {
      return {...empty};
    }
  }

  update(patch) {
    const current = this.get();
    const value = {
      projects: patch.projects === undefined ? current.projects : normalizeProjects(patch.projects),
      jiraBaseUrl: patch.jiraBaseUrl === undefined ? current.jiraBaseUrl : normalizeBaseUrl(patch.jiraBaseUrl),
      cloudId: patch.cloudId === undefined ? current.cloudId : String(patch.cloudId || ''),
      groupings: patch.groupings === undefined ? current.groupings : patch.groupings,
      llm: patch.llm === undefined ? current.llm : normalizeLlm(patch.llm)
    };
    const temporary = `${this.path}.tmp`;
    writeFileSync(temporary, `${JSON.stringify(value, null, 2)}\n`, {mode: 0o600});
    renameSync(temporary, this.path);
    return value;
  }
}

export function normalizeLlm(value={}) {
  const provider=value.provider==='external'?'external':'ollama';
  return {provider,ollamaUrl:String(value.ollamaUrl||defaultLlm.ollamaUrl).replace(/\/$/,''),ollamaModel:String(value.ollamaModel||defaultLlm.ollamaModel),external:{baseUrl:String(value.external?.baseUrl||defaultLlm.external.baseUrl).replace(/\/$/,''),model:String(value.external?.model||'')}};
}

export function normalizeProjects(projects) {
  const normalized = (Array.isArray(projects) ? projects : []).map(project => ({key: String(project?.key || '').trim().toUpperCase(), name: String(project?.name || project?.key || '').trim()})).filter(project => /^[A-Z][A-Z0-9_]*$/.test(project.key));
  return [...new Map(normalized.map(project => [project.key, project])).values()];
}

export function normalizeBaseUrl(value) {
  const raw = String(value || '').trim();
  if (!raw) return '';
  const url = new URL(raw);
  if (url.protocol !== 'https:') throw new Error('Jira base URL must use HTTPS');
  return url.origin;
}
