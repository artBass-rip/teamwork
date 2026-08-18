const systemPrompt = `You classify Jira issues by their technical meaning, not by isolated words. Return strict JSON: {"groups":[{"name":"...","description":"...","examples":["..."],"issueKeys":["ABC-1"]}]}. Create a small coherent hierarchy level of semantic workstreams. Every issue key must appear in exactly one group. Do not create groups for incidental technologies when the task's main intent belongs elsewhere.`;

export class LlmAnalyzer {
  constructor({settings, apiKey = '', fetchImpl = fetch}) { this.settings = settings; this.apiKey = apiKey; this.fetch = fetchImpl; }

  async analyze(project, issues) {
    const provider = this.settings.provider || 'ollama';
    const payload = issues.map(issue => ({key:issue.key,title:issue.title,type:issue.type,labels:issue.labels,components:issue.components,description:String(issue.description || '').slice(0,500)}));
    const groups = [];
    for (let offset=0; offset<payload.length; offset+=25) {
      const batch=payload.slice(offset,offset+25);
      const prompt = `Project ${project.key} (${project.name}). Classify this batch semantically. Reuse concise, general workstream names that are likely to apply to other batches:\n${JSON.stringify(batch)}`;
      const text = provider === 'external' ? await this.external(prompt) : await this.ollama(prompt);
      const parsed = parseJson(text);
      groups.push(...normalizeGroups(parsed.groups,new Set(batch.map(issue=>issue.key))));
    }
    return mergeGroups(groups);
  }

  async ollama(prompt) {
    const response = await this.fetch(`${this.settings.ollamaUrl || 'http://ollama:11434'}/api/chat`, {method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({model:this.settings.ollamaModel || 'qwen2.5:3b',stream:false,format:'json',messages:[{role:'system',content:systemPrompt},{role:'user',content:prompt}]})});
    const value = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(value.error || `Ollama HTTP ${response.status}. Pull the configured model in the Ollama container.`);
    return value.message?.content || '';
  }

  async external(prompt) {
    if (!this.settings.external?.baseUrl || !this.settings.external?.model || !this.apiKey) throw new Error('External LLM is not fully configured');
    const response = await this.fetch(`${this.settings.external.baseUrl.replace(/\/$/,'')}/chat/completions`, {method:'POST',headers:{'content-type':'application/json',authorization:`Bearer ${this.apiKey}`},body:JSON.stringify({model:this.settings.external.model,response_format:{type:'json_object'},messages:[{role:'system',content:systemPrompt},{role:'user',content:prompt}]})});
    const value = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(value.error?.message || `External LLM HTTP ${response.status}`);
    return value.choices?.[0]?.message?.content || '';
  }
}

function parseJson(text) {
  const cleaned = String(text || '').replace(/^```json\s*|\s*```$/g,'').trim();
  try { return JSON.parse(cleaned); } catch { throw new Error('LLM returned invalid grouping JSON'); }
}

function normalizeGroups(groups, validKeys) {
  const assigned = new Set();
  return (Array.isArray(groups) ? groups : []).map(group => {
    const issueKeys = [...new Set((group.issueKeys || []).filter(key => validKeys.has(key) && !assigned.has(key)))];
    issueKeys.forEach(key => assigned.add(key));
    return {name:String(group.name || '').trim(),description:String(group.description || '').trim(),examples:(group.examples || []).map(String).slice(0,5),issueKeys,source:'analyzer'};
  }).filter(group => group.name && group.issueKeys.length);
}

function mergeGroups(groups) {
  const merged=new Map();
  for (const group of groups) {
    const identity=group.name.toLocaleLowerCase();
    const current=merged.get(identity);
    if (!current) { merged.set(identity,{...group}); continue; }
    current.issueKeys=[...new Set([...current.issueKeys,...group.issueKeys])];
    current.examples=[...new Set([...current.examples,...group.examples])].slice(0,5);
    if (!current.description && group.description) current.description=group.description;
  }
  return [...merged.values()];
}
