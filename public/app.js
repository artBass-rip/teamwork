import {groupMarkdownBySprint} from './sprints.js';
import {plainTaskText, writeRichClipboard} from './clipboard.js';

const $ = selector => document.querySelector(selector);
const escapeHtml = value => value.replace(/[&<>"']/g, char => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]));
const slugCounts = new Map();
let selectedIssueKey = null;
let commentCounts = {};
let commentsByIssue = {};
let taskLabelsByIssue = {};
let labelCatalog = [];
let sourceDocument = '';
let sourceConfig = null;
let viewerMode = 'document';

function slug(value) {
  const base = value.toLowerCase().replace(/<[^>]+>/g, '').replace(/[^\p{L}\p{N}]+/gu, '-').replace(/^-|-$/g, '') || 'section';
  const count = slugCounts.get(base) || 0;
  slugCounts.set(base, count + 1);
  return count ? `${base}-${count + 1}` : base;
}

function inline(value) {
  return escapeHtml(value)
    .replace(/\[([^\]]+)\]\((https?:\/\/[^)]+)\)/g, '<a href="$2" target="_blank" rel="noreferrer">$1</a>')
    .replace(/`([^`]+)`/g, '<code>$1</code>')
    .replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>')
    .replace(/\*(.*?)\*/g, '<em>$1</em>');
}

function parseMarkdown(markdown) {
  slugCounts.clear();
  const lines = markdown.replace(/\r/g, '').split('\n');
  const headings = [];
  const html = [];
  let paragraph = [];
  let list = null;
  let code = null;

  const flushParagraph = () => {
    if (paragraph.length) html.push(`<p>${paragraph.map(inline).join('<br>')}</p>`);
    paragraph = [];
  };
  const closeList = () => { if (list) html.push(`</${list}>`); list = null; };

  for (const line of lines) {
    if (line.startsWith('```')) {
      flushParagraph(); closeList();
      if (code === null) code = [];
      else { html.push(`<pre><code>${escapeHtml(code.join('\n'))}</code></pre>`); code = null; }
      continue;
    }
    if (code !== null) { code.push(line); continue; }
    const heading = /^(#{1,6})\s+(.+)$/.exec(line);
    if (heading) {
      flushParagraph(); closeList();
      const level = heading[1].length;
      const text = heading[2].replace(/\*\*/g, '');
      const id = slug(text);
      const plainText = text.replace(/\[([^\]]+)\]\([^)]+\)/g, '$1');
      const issueKey = level === 5 ? plainText.match(/\b[A-Z][A-Z0-9_]*-\d+\b/)?.[0] : null;
      headings.push({level, text: plainText, id});
      const collapsible = level >= 2 && level <= 5;
      const commentAction = issueKey ? `<button class="comment-trigger" data-issue-key="${issueKey}" aria-label="Открыть комментарии к ${issueKey}" title="Локальные комментарии"><span>💬</span><b hidden>0</b></button>` : '';
      const copyAction = issueKey ? `<button class="copy-trigger" data-issue-key="${issueKey}" aria-label="Копировать ${issueKey}" title="Копировать задачу с Jira-ссылкой"><span>⧉</span></button>` : '';
      html.push(`<h${level} id="${id}" data-level="${level}"${issueKey ? ` data-issue-key="${issueKey}"` : ''}>${collapsible ? '<button class="fold" aria-label="Свернуть раздел">⌄</button>' : ''}<span>${inline(heading[2])}</span>${copyAction}${commentAction}</h${level}>`);
      continue;
    }
    const item = /^\s*([-*]|\d+\.)\s+(.+)$/.exec(line);
    if (item) {
      flushParagraph();
      const nextList = item[1].endsWith('.') ? 'ol' : 'ul';
      if (list !== nextList) { closeList(); list = nextList; html.push(`<${list}>`); }
      html.push(`<li>${inline(item[2])}</li>`);
      continue;
    }
    if (!line.trim()) { flushParagraph(); closeList(); }
    else paragraph.push(line);
  }
  flushParagraph(); closeList();
  return {html: html.join('\n'), headings};
}

function buildOutline(headings) {
  const visible = headings.filter(item => item.level >= 2 && item.level <= 5);
  const root = {level: 1, children: []};
  const stack = [root];
  for (const item of visible) {
    while (stack.length > 1 && stack.at(-1).level >= item.level) stack.pop();
    const node = {...item, children: []};
    stack.at(-1).children.push(node);
    stack.push(node);
  }
  const renderNodes = nodes => nodes.map(node => {
    const hasChildren = node.children.length > 0;
    return `<div class="outline-node" data-level="${node.level}"><div class="outline-item">${hasChildren ? '<button class="outline-fold" aria-label="Свернуть ветку">⌄</button>' : '<span class="outline-spacer"></span>'}<a href="#${node.id}" data-target="${node.id}"><span>${escapeHtml(node.text)}</span></a></div>${hasChildren ? `<div class="outline-children">${renderNodes(node.children)}</div>` : ''}</div>`;
  }).join('');
  $('#outline').innerHTML = renderNodes(root.children);
  document.querySelectorAll('.outline-fold').forEach(button => button.addEventListener('click', () => {
    const node = button.closest('.outline-node');
    const collapsed = node.classList.toggle('tree-collapsed');
    button.setAttribute('aria-label', collapsed ? 'Развернуть ветку' : 'Свернуть ветку');
  }));
}

function nodesInSection(heading) {
  const level = Number(heading.dataset.level);
  const nodes = [];
  for (let node = heading.nextElementSibling; node; node = node.nextElementSibling) {
    if (/^H[1-6]$/.test(node.tagName) && Number(node.dataset.level || node.tagName.slice(1)) <= level) break;
    nodes.push(node);
  }
  return nodes;
}

function refreshDocumentVisibility() {
  const stack = [];
  for (const node of $('#markdown').children) {
    const headingMatch = /^H([1-6])$/.exec(node.tagName);
    if (headingMatch) {
      const level = Number(headingMatch[1]);
      while (stack.length && stack.at(-1).level >= level) stack.pop();
      node.hidden = stack.some(item => item.collapsed);
      stack.push({level, collapsed: node.classList.contains('collapsed')});
    } else {
      node.hidden = stack.some(item => item.collapsed);
    }
  }
}

function setCollapsed(heading, collapsed) {
  heading.classList.toggle('collapsed', collapsed);
  heading.querySelector('.fold')?.setAttribute('aria-label', collapsed ? 'Развернуть раздел' : 'Свернуть раздел');
  refreshDocumentVisibility();
}

function wireViewer(headings) {
  buildOutline(headings);
  document.querySelectorAll('.markdown h2,.markdown h3,.markdown h4,.markdown h5').forEach(heading => {
    heading.querySelector('.fold')?.addEventListener('click', event => {
      event.preventDefault(); setCollapsed(heading, !heading.classList.contains('collapsed'));
    });
  });
  const observer = new IntersectionObserver(entries => {
    const current = entries.filter(entry => entry.isIntersecting).sort((a,b) => a.boundingClientRect.top-b.boundingClientRect.top)[0];
    if (!current) return;
    document.querySelectorAll('.outline a').forEach(link => link.classList.toggle('active', link.dataset.target === current.target.id));
  }, {rootMargin: '-12% 0px -75% 0px'});
  document.querySelectorAll('.markdown h2,.markdown h3,.markdown h4,.markdown h5').forEach(node => observer.observe(node));
  document.querySelectorAll('.comment-trigger').forEach(button => button.addEventListener('click', event => {
    event.preventDefault();
    event.stopPropagation();
    openComments(button.dataset.issueKey);
  }));
  document.querySelectorAll('.copy-trigger').forEach(button => button.addEventListener('click', event => {
    event.preventDefault();
    event.stopPropagation();
    copyTask(button.closest('h5'), button);
  }));
  renderTaskLabels();
  refreshCommentBadges();
}

async function copyTask(heading, button) {
  const title = heading.querySelector(':scope > span')?.textContent.trim() || heading.textContent.trim();
  const url = heading.querySelector(':scope > span a')?.href || '';
  const content = nodesInSection(heading).filter(node => !node.classList.contains('inline-comments'));
  const container = document.createElement('div');
  const titleElement = document.createElement('p');
  titleElement.innerHTML = `<strong>${heading.querySelector(':scope > span')?.innerHTML || escapeHtml(title)}</strong>`;
  container.append(titleElement, ...content.map(node => {
    const clone = node.cloneNode(true);
    clone.hidden = false;
    clone.classList.remove('search-hidden');
    clone.querySelectorAll('button,.task-labels').forEach(item => item.remove());
    return clone;
  }));
  const bodyText = content.map(node => node.textContent.trim()).filter(Boolean).join('\n');
  try {
    await writeRichClipboard(container.innerHTML, plainTaskText(title, url, bodyText));
    button.classList.add('copied');
    button.querySelector('span').textContent = '✓';
    button.title = 'Скопировано';
    setTimeout(() => {
      button.classList.remove('copied');
      button.querySelector('span').textContent = '⧉';
      button.title = 'Копировать задачу с Jira-ссылкой';
    }, 1600);
  } catch (error) {
    button.title = `Не удалось скопировать: ${error.message}`;
  }
}

function renderTaskLabels() {
  document.querySelectorAll('.task-labels').forEach(node => node.remove());
  document.querySelectorAll('.markdown h5[data-issue-key]').forEach(heading => {
    const labels = taskLabelsByIssue[heading.dataset.issueKey] || [];
    if (!labels.length) return;
    const wrapper = document.createElement('span');
    wrapper.className = 'task-labels';
    wrapper.innerHTML = labels.map((label, index) => `<button class="task-label${index === 0 ? ' grouping-label' : ''}" title="${index === 0 ? 'Группирующая метка' : 'Локальная метка'}">${escapeHtml(label)}</button>`).join('');
    heading.querySelector('.comment-trigger').before(wrapper);
    wrapper.querySelectorAll('button').forEach(button => button.addEventListener('click', event => { event.preventDefault(); openComments(heading.dataset.issueKey); }));
  });
}

async function loadAllLabels() {
  const result = await fetch('/api/labels').then(response => response.json());
  taskLabelsByIssue = result.labels || {};
  labelCatalog = result.catalog || buildLabelCatalog(taskLabelsByIssue);
  renderTaskLabels();
  renderLabelCatalog();
}

function buildLabelCatalog(labelsByIssue) {
  const counts = new Map();
  Object.values(labelsByIssue).flat().forEach(label => counts.set(label, (counts.get(label) || 0) + 1));
  return [...counts].map(([name, issues]) => ({name, issues})).sort((a, b) => a.name.localeCompare(b.name, 'ru'));
}

function renderLabelCatalog() {
  $('#label-options').innerHTML = labelCatalog.map(item => `<option value="${escapeHtml(item.name)}"></option>`).join('');
  $('#label-catalog-count').textContent = labelCatalog.length;
  $('#label-catalog-list').innerHTML = labelCatalog.length ? labelCatalog.map(item => `<div class="catalog-label"><button class="catalog-select" data-label="${escapeHtml(item.name)}"><span>${escapeHtml(item.name)}</span><small>${item.issues}</small></button><button class="catalog-delete" data-label="${escapeHtml(item.name)}" aria-label="Удалить метку ${escapeHtml(item.name)} из системы">Удалить</button></div>`).join('') : '<span class="labels-empty">Каталог пока пуст</span>';
  document.querySelectorAll('.catalog-select').forEach(button => button.addEventListener('click', () => addExistingLabel(button.dataset.label)));
  document.querySelectorAll('.catalog-delete').forEach(button => button.addEventListener('click', () => deleteCatalogLabel(button.dataset.label)));
}

function addExistingLabel(label) {
  if (!selectedIssueKey) return;
  const current = taskLabelsByIssue[selectedIssueKey] || [];
  if (current.some(item => item.toLocaleLowerCase() === label.toLocaleLowerCase())) {
    $('#label-message').textContent = 'Эта метка уже назначена задаче';
    return;
  }
  saveLabels([...current, label]);
}

async function deleteCatalogLabel(label) {
  const usage = labelCatalog.find(item => item.name === label)?.issues || 0;
  if (!confirm(`Удалить метку «${label}» у ${usage} задач?`)) return;
  $('#label-message').textContent = 'Удаление метки и перегруппировка…';
  const response = await fetch(`/api/label-catalog/${encodeURIComponent(label)}`, {method: 'DELETE'});
  const result = await response.json();
  if (!response.ok) { $('#label-message').textContent = result.error; return; }
  taskLabelsByIssue = result.labels || {};
  labelCatalog = result.catalog || [];
  $('#label-message').textContent = `Метка «${label}» удалена у ${result.removed.issues} задач`;
  renderLabelEditor();
  renderLabelCatalog();
  await load();
}

function refreshCommentBadges() {
  document.querySelectorAll('.comment-trigger').forEach(button => {
    const count = commentCounts[button.dataset.issueKey] || 0;
    const badge = button.querySelector('b');
    badge.textContent = count;
    badge.hidden = count === 0;
    button.classList.toggle('has-comments', count > 0);
  });
  renderInlineComments();
}

function renderInlineComments() {
  document.querySelectorAll('.inline-comments').forEach(node => node.remove());
  document.querySelectorAll('.markdown h5[data-issue-key]').forEach(heading => {
    const issueKey = heading.dataset.issueKey;
    const comments = commentsByIssue[issueKey] || [];
    if (!comments.length) return;
    const block = document.createElement('div');
    block.className = 'inline-comments';
    block.dataset.issueKey = issueKey;
    block.innerHTML = `<div class="inline-comments-label"><span>Локальные комментарии · ${comments.length}</span><button data-open-comments="${issueKey}">Открыть</button></div>${comments.map(comment => `<div class="inline-comment"><time datetime="${comment.createdAt}">${new Date(comment.createdAt).toLocaleString()}</time><p>${escapeHtml(comment.text).replace(/\n/g, '<br>')}</p></div>`).join('')}`;
    heading.after(block);
    block.querySelector('[data-open-comments]').addEventListener('click', () => openComments(issueKey));
  });
  refreshDocumentVisibility();
}

async function loadAllComments() {
  const result = await fetch('/api/comments').then(response => response.json());
  commentsByIssue = result.comments || {};
  commentCounts = Object.fromEntries(Object.entries(commentsByIssue).map(([key, comments]) => [key, comments.length]));
  refreshCommentBadges();
}

async function openComments(issueKey) {
  selectedIssueKey = issueKey;
  $('#comments-task').textContent = issueKey;
  $('#comments-panel').hidden = false;
  $('.document-view').classList.add('comments-open');
  $('#comment-message').textContent = '';
  $('#label-message').textContent = '';
  renderLabelEditor();
  await loadComments();
}

function renderLabelEditor() {
  const labels = taskLabelsByIssue[selectedIssueKey] || [];
  $('#task-labels-editor').innerHTML = labels.length ? labels.map((label, index) => `<span class="editable-label"><span>${escapeHtml(label)}</span><button class="remove-label" data-label-index="${index}" aria-label="Снять метку ${escapeHtml(label)} с задачи">×</button></span>`).join('') : '<span class="labels-empty">Метки не назначены — используется theme</span>';
  document.querySelectorAll('.remove-label').forEach(button => button.addEventListener('click', () => {
    saveLabels(labels.filter((_, index) => index !== Number(button.dataset.labelIndex)));
  }));
}

async function saveLabels(labels) {
  if (!selectedIssueKey) return;
  const issueKey = selectedIssueKey;
  $('#label-message').textContent = 'Перегруппировка документа…';
  const response = await fetch(`/api/labels/${encodeURIComponent(issueKey)}`, {method: 'PUT', headers: {'content-type': 'application/json'}, body: JSON.stringify({labels})});
  const result = await response.json();
  if (!response.ok) { $('#label-message').textContent = result.error; return; }
  taskLabelsByIssue[issueKey] = result.labels;
  labelCatalog = buildLabelCatalog(taskLabelsByIssue);
  $('#label-message').textContent = result.sync?.lastError ? `Метки сохранены, ошибка перегруппировки: ${result.sync.lastError}` : 'Метки сохранены, документ перегруппирован';
  renderLabelEditor();
  renderLabelCatalog();
  await load();
}

function closeComments() {
  selectedIssueKey = null;
  $('#comments-panel').hidden = true;
  $('.document-view').classList.remove('comments-open');
}

async function loadComments() {
  if (!selectedIssueKey) return;
  const issueKey = selectedIssueKey;
  const result = await fetch(`/api/comments/${encodeURIComponent(issueKey)}`).then(response => response.json());
  if (issueKey !== selectedIssueKey) return;
  commentsByIssue[issueKey] = result.comments;
  commentCounts[issueKey] = result.comments.length;
  refreshCommentBadges();
  $('#comments-list').innerHTML = result.comments.length ? result.comments.map(comment => `
    <article class="comment-item">
      <time datetime="${comment.createdAt}">${new Date(comment.createdAt).toLocaleString()}</time>
      <p>${escapeHtml(comment.text).replace(/\n/g, '<br>')}</p>
      <button class="delete-comment" data-comment-id="${comment.id}" aria-label="Удалить комментарий">Удалить</button>
    </article>`).join('') : '<div class="comments-empty"><span>💬</span><strong>Комментариев пока нет</strong><p>Они хранятся локально и не отправляются в Jira.</p></div>';
  document.querySelectorAll('.delete-comment').forEach(button => button.addEventListener('click', () => deleteComment(button.dataset.commentId)));
}

async function deleteComment(commentId) {
  const issueKey = selectedIssueKey;
  const response = await fetch(`/api/comments/${encodeURIComponent(issueKey)}/${encodeURIComponent(commentId)}`, {method: 'DELETE'});
  if (!response.ok || issueKey !== selectedIssueKey) return;
  commentCounts[issueKey] = Math.max(0, (commentCounts[issueKey] || 0) - 1);
  refreshCommentBadges();
  await loadComments();
}

async function load() {
  const [doc, cfg, status] = await Promise.all([
    fetch('/api/document').then(response => response.text()),
    fetch('/api/config').then(response => response.json()),
    fetch('/api/status').then(response => response.json())
  ]);
  sourceDocument = doc;
  sourceConfig = cfg;
  $('#editor').value = JSON.stringify(cfg, null, 2);
  renderViewer();
  await Promise.all([loadAllComments(), loadAllLabels()]);
  showStatus(status);
}

function renderViewer() {
  const markdown = viewerMode === 'sprints' ? groupMarkdownBySprint(sourceDocument, sourceConfig) : sourceDocument;
  const parsed = parseMarkdown(markdown);
  $('#markdown').innerHTML = parsed.html;
  $('#document-meta').textContent = `${viewerMode === 'sprints' ? 'По спринтам' : 'Документ'} · ${markdown.split('\n').length.toLocaleString()} строк · ${parsed.headings.length} разделов`;
  $('#search').value = '';
  wireViewer(parsed.headings);
}

function showStatus(state) {
  const status = $('#status');
  const sync = $('#sync');
  sync.disabled = Boolean(state.running);
  status.className = `status ${state.running ? 'running' : state.lastError ? 'failed' : state.lastSuccess ? 'success' : ''}`;
  status.textContent = state.running ? 'Обновление…' : state.lastSuccess ? `${state.issues} задач · ${new Date(state.lastSuccess).toLocaleString()}` : 'Ожидание синхронизации';
  const banner = $('#error-banner');
  if (state.lastError) {
    const unauthorized = /401|unauthorized/i.test(state.lastError);
    $('#error-title').textContent = unauthorized ? 'MCP не авторизован' : 'Ошибка синхронизации';
    $('#error-detail').textContent = unauthorized
      ? 'Docker MCP Gateway отклонил запрос. Перезапустите сервис через ./start.sh; авторизацией Atlassian управляет профиль Docker MCP.'
      : state.lastError;
    banner.hidden = false;
  }
}

async function loadLogs() {
  const level = $('#log-level').value;
  const result = await fetch(`/api/logs?limit=300${level ? `&level=${encodeURIComponent(level)}` : ''}`).then(response => response.json());
  $('#log-entries').innerHTML = result.entries.map(entry => {
    const context = Object.fromEntries(Object.entries(entry).filter(([key]) => !['timestamp','level','event','message'].includes(key) && entry[key] != null));
    const details = Object.keys(context).length ? `<details><summary>${escapeHtml(entry.message)}</summary><pre>${escapeHtml(JSON.stringify(context,null,2))}</pre></details>` : escapeHtml(entry.message);
    return `<div class="log-row"><time>${new Date(entry.timestamp).toLocaleTimeString()}</time><span class="log-level ${entry.level}">${entry.level}</span><code>${escapeHtml(entry.event)}</code><div>${details}</div></div>`;
  }).join('') || '<div class="empty-logs">Событий пока нет.</div>';
}

document.querySelectorAll('.tab').forEach(button => button.addEventListener('click', () => {
  document.querySelectorAll('.tab,.view').forEach(node => node.classList.remove('active'));
  button.classList.add('active'); $(`#${button.dataset.view}`).classList.add('active');
  if (button.dataset.mode) {
    viewerMode = button.dataset.mode;
    if (sourceDocument && sourceConfig) renderViewer();
  }
  if (button.dataset.view === 'logs') loadLogs();
}));
$('#sync').addEventListener('click', async () => { showStatus({running:true}); const state = await fetch('/api/sync',{method:'POST'}).then(r=>r.json()); showStatus(state); await load(); });
$('#save').addEventListener('click', async () => { try { const value=JSON.parse($('#editor').value); const response=await fetch('/api/config',{method:'PUT',headers:{'content-type':'application/json'},body:JSON.stringify(value)}); const result=await response.json(); if(!response.ok) throw Error(result.error); $('#message').textContent='Конфигурация сохранена'; } catch(error) { $('#message').textContent=`Ошибка: ${error.message}`; } });
$('#dismiss-error').addEventListener('click', () => $('#error-banner').hidden = true);
$('#collapse-all').addEventListener('click', () => {
  document.querySelectorAll('.markdown h2,.markdown h3,.markdown h4,.markdown h5').forEach(heading => heading.classList.add('collapsed'));
  refreshDocumentVisibility();
});
$('#expand-all').addEventListener('click', () => {
  document.querySelectorAll('.markdown h2,.markdown h3,.markdown h4,.markdown h5').forEach(heading => heading.classList.remove('collapsed'));
  refreshDocumentVisibility();
});
$('#toggle-outline').addEventListener('click', () => $('.document-view').classList.toggle('outline-hidden'));
$('#show-outline').addEventListener('click', () => $('.document-view').classList.toggle('outline-hidden'));
$('#search').addEventListener('input', event => {
  const query = event.target.value.trim().toLowerCase();
  const filterOutlineNode = node => {
    const ownMatch = node.querySelector(':scope > .outline-item a')?.textContent.toLowerCase().includes(query);
    const childMatches = [...node.querySelectorAll(':scope > .outline-children > .outline-node')].map(filterOutlineNode);
    const visible = !query || ownMatch || childMatches.some(Boolean);
    node.hidden = !visible;
    if (query && childMatches.some(Boolean)) node.classList.remove('tree-collapsed');
    return visible;
  };
  document.querySelectorAll('#outline > .outline-node').forEach(filterOutlineNode);
  document.querySelectorAll('.markdown h5').forEach(heading => {
    const match = !query || `${heading.textContent} ${nodesInSection(heading).map(n=>n.textContent).join(' ')}`.toLowerCase().includes(query);
    heading.classList.toggle('search-hidden', !match);
    nodesInSection(heading).forEach(node => node.classList.toggle('search-hidden', !match));
  });
});
$('#refresh-logs').addEventListener('click', loadLogs);
$('#log-level').addEventListener('change', loadLogs);
$('#close-comments').addEventListener('click', closeComments);
$('#comment-form').addEventListener('submit', async event => {
  event.preventDefault();
  if (!selectedIssueKey) return;
  const issueKey = selectedIssueKey;
  const text = $('#comment-text').value.trim();
  if (!text) return;
  const response = await fetch(`/api/comments/${encodeURIComponent(issueKey)}`, {method: 'POST', headers: {'content-type': 'application/json'}, body: JSON.stringify({text})});
  const result = await response.json();
  if (!response.ok) { $('#comment-message').textContent = result.error; return; }
  if (issueKey !== selectedIssueKey) return;
  $('#comment-text').value = '';
  commentCounts[issueKey] = (commentCounts[issueKey] || 0) + 1;
  refreshCommentBadges();
  await loadComments();
});
$('#delete-comments').addEventListener('click', async () => {
  if (!selectedIssueKey || !(commentCounts[selectedIssueKey] || 0)) return;
  const issueKey = selectedIssueKey;
  if (!confirm(`Удалить все локальные комментарии к ${issueKey}?`)) return;
  const response = await fetch(`/api/comments/${encodeURIComponent(issueKey)}`, {method: 'DELETE'});
  if (!response.ok || issueKey !== selectedIssueKey) return;
  commentCounts[issueKey] = 0;
  refreshCommentBadges();
  await loadComments();
});
$('#label-form').addEventListener('submit', event => {
  event.preventDefault();
  const label = $('#label-input').value.trim();
  if (!label || !selectedIssueKey) return;
  const current = taskLabelsByIssue[selectedIssueKey] || [];
  $('#label-input').value = '';
  saveLabels([...current, label]);
});

load();
setInterval(() => fetch('/api/status').then(response => response.json()).then(showStatus), 5000);
