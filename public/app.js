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
let activeProject = '';
let availableProjects = [];
let selectedProjects = [];
let language = localStorage.getItem('teamwork-language') || 'ru';

const messages = {
  ru: {projects:'Проекты',logs:'Журнал',integration:'Интеграция',configuration:'Конфигурация',syncNow:'Обновить сейчас',availableProjects:'Доступные проекты',projectsHint:'Выберите проекты, которые TeamWork будет опрашивать и анализировать по расписанию.',searchProjects:'Найти проект',saveSelection:'Сохранить выбор',selected:n=>`Выбрано: ${n}`,waiting:'Ожидание синхронизации',updating:'Обновление…',issues:n=>`${n} задач`,noProjects:'Доступных проектов не найдено',selectOne:'Выберите хотя бы один проект',selectionSaved:'Выбор сохранён, документы обновлены',structure:'Структура',searchDocument:'Найти в документе',collapse:'Свернуть',expand:'Развернуть',analyzeProject:'Анализировать',eventLog:'Журнал событий',eventsHint:'Последние события приложения и MCP-синхронизации. Секреты не записываются.',refresh:'Обновить',atlassianIntegration:'Интеграция Atlassian',connectAtlassian:'Подключить Atlassian',refreshStatus:'Обновить статус',disconnect:'Отключить',jiraBaseUrl:'Адрес Jira',saveUrl:'Сохранить адрес',jiraBaseUrlHint:'Используется для ссылок на задачи и хранится в рабочей директории проекта.',llmTitle:'Модель анализатора',llmHint:'Подключение внешней LLM не включает её автоматически.',externalLlm:'Внешняя LLM',saveLlm:'Сохранить подключение',groupingPattern:'Правила группировки',configHint:'Правила независимы для каждого проекта. Анализатор только добавляет новые темы.',save:'Сохранить'},
  en: {projects:'Projects',logs:'Logs',integration:'Integration',configuration:'Configuration',syncNow:'Sync now',availableProjects:'Available projects',projectsHint:'Select the projects TeamWork should poll and analyze on schedule.',searchProjects:'Search projects',saveSelection:'Save selection',selected:n=>`Selected: ${n}`,waiting:'Waiting for synchronization',updating:'Updating…',issues:n=>`${n} issues`,noProjects:'No available projects found',selectOne:'Select at least one project',selectionSaved:'Selection saved and documents refreshed',structure:'Outline',searchDocument:'Search document',collapse:'Collapse',expand:'Expand',analyzeProject:'Analyze',eventLog:'Event log',eventsHint:'Recent application and MCP synchronization events. Secrets are never logged.',refresh:'Refresh',atlassianIntegration:'Atlassian integration',connectAtlassian:'Connect Atlassian',refreshStatus:'Refresh status',disconnect:'Disconnect',jiraBaseUrl:'Jira address',saveUrl:'Save address',jiraBaseUrlHint:'Used for issue links and stored in the project working directory.',llmTitle:'Analyzer model',llmHint:'Connecting an external LLM does not activate it automatically.',externalLlm:'External LLM',saveLlm:'Save connection',groupingPattern:'Grouping rules',configHint:'Rules are independent for every project. The analyzer only appends new themes.',save:'Save'}
};
const t = (key, value) => typeof messages[language][key] === 'function' ? messages[language][key](value) : messages[language][key] || key;

function applyLanguage() {
  document.documentElement.lang = language;
  document.querySelectorAll('[data-i18n]').forEach(node => { node.textContent = t(node.dataset.i18n); });
  document.querySelectorAll('[data-i18n-placeholder]').forEach(node => { node.placeholder = t(node.dataset.i18nPlaceholder); });
  $('#language-toggle').textContent = language === 'ru' ? 'EN' : 'RU';
  renderProjects();
}

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
      const issueKey = level >= 2 ? plainText.match(/\b[A-Z][A-Z0-9_]*-\d+\b/)?.[0] : null;
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

function renderProjectTabs() {
  $('#project-tabs').innerHTML = selectedProjects.map(project => `<button class="tab project-tab${project.key === activeProject ? ' active' : ''}" data-project="${escapeHtml(project.key)}">${escapeHtml(project.key)}</button>`).join('');
  document.querySelectorAll('.project-tab').forEach(button => button.addEventListener('click', () => openProject(button.dataset.project)));
}

function renderProjects() {
  const container = $('#projects-list');
  if (!container) return;
  const query = ($('#project-search')?.value || '').trim().toLowerCase();
  const selected = new Set(selectedProjects.map(project => project.key));
  const visible = availableProjects.filter(project => `${project.key} ${project.name}`.toLowerCase().includes(query));
  container.innerHTML = visible.length ? visible.map(project => `<label class="project-option"><input type="checkbox" value="${escapeHtml(project.key)}"${selected.has(project.key) ? ' checked' : ''}><span class="project-key">${escapeHtml(project.key)}</span><span><strong>${escapeHtml(project.name)}</strong><small>${escapeHtml(project.type || 'Jira')}</small></span></label>`).join('') : `<p>${t('noProjects')}</p>`;
  $('#projects-selection-count').textContent = t('selected', selected.size);
  container.querySelectorAll('input').forEach(input => input.addEventListener('change', () => {
    const project = availableProjects.find(item => item.key === input.value);
    selectedProjects = input.checked ? [...selectedProjects, project] : selectedProjects.filter(item => item.key !== input.value);
    $('#projects-selection-count').textContent = t('selected', selectedProjects.length);
  }));
}

async function loadProjects() {
  const response = await fetch('/api/projects');
  const result = await response.json();
  availableProjects = result.projects || [];
  selectedProjects = result.selected || [];
  if (result.error) $('#projects-message').textContent = result.error;
  renderProjectTabs();
  renderProjects();
}

function activateView(view) {
  document.querySelectorAll('.view').forEach(node => node.classList.remove('active'));
  $(`#${view}`).classList.add('active');
  document.querySelectorAll('.tabs > .tab').forEach(node => node.classList.toggle('active', node.dataset.view === view));
}

async function openProject(projectKey) {
  activeProject = projectKey;
  activateView('document');
  renderProjectTabs();
  await loadProjectGrouping();
  sourceDocument = await fetch(`/api/document?project=${encodeURIComponent(projectKey)}&mode=${viewerMode}`).then(response => response.text());
  renderViewer();
  await Promise.all([loadAllComments(), loadAllLabels()]);
}

async function load() {
  const status = await fetch('/api/status').then(response => response.json());
  await loadProjects();
  activeProject = activeProject || selectedProjects[0]?.key || '';
  if (activeProject) { await loadProjectGrouping(); sourceDocument = await fetch(`/api/document?project=${encodeURIComponent(activeProject)}&mode=${viewerMode}`).then(response => response.text()); }
  if (sourceDocument) renderViewer();
  await Promise.all([loadAllComments(), loadAllLabels()]);
  showStatus(status);
}

async function loadProjectGrouping() {
  if (!activeProject) { sourceConfig = {grouping: {themes: []}}; $('#editor').value = JSON.stringify(sourceConfig.grouping, null, 2); return; }
  const result = await fetch(`/api/grouping?project=${encodeURIComponent(activeProject)}`).then(response => response.json());
  sourceConfig = {grouping: result.grouping};
  $('#editor').value = JSON.stringify(result.grouping, null, 2);
}

function renderViewer() {
  const markdown = sourceDocument;
  const parsed = parseMarkdown(markdown);
  $('#markdown').innerHTML = parsed.html;
  $('#document-meta').textContent = `${activeProject} · ${viewerMode === 'sprints' ? 'Sprints' : 'Backlog'} · ${markdown.split('\n').length.toLocaleString()} ${language === 'ru' ? 'строк' : 'lines'} · ${parsed.headings.length} ${language === 'ru' ? 'разделов' : 'sections'}`;
  $('#search').value = '';
  wireViewer(parsed.headings);
}

function showStatus(state) {
  const status = $('#status');
  const sync = $('#sync');
  sync.disabled = Boolean(state.running);
  status.className = `status ${state.running ? 'running' : state.lastError ? 'failed' : state.lastSuccess ? 'success' : ''}`;
  status.textContent = state.running ? t('updating') : state.lastSuccess ? `${t('issues', state.issues)} · ${new Date(state.lastSuccess).toLocaleString(language)}` : t('waiting');
  const banner = $('#error-banner');
  if (state.lastError) {
    const unauthorized = /401|unauthorized/i.test(state.lastError);
    $('#error-title').textContent = unauthorized ? 'MCP не авторизован' : 'Ошибка синхронизации';
    $('#error-detail').textContent = unauthorized
      ? 'Встроенный Jira MCP не авторизован. Откройте вкладку «Интеграция» и подключите Atlassian.'
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

async function loadJiraAuth() {
  const [response, workspaceResponse, llmResponse] = await Promise.all([fetch('/api/jira-auth/status'), fetch('/api/workspace'), fetch('/api/llm')]);
  const [state, workspace, llm] = await Promise.all([response.json(), workspaceResponse.json(), llmResponse.json()]);
  $('#jira-base-url').value = workspace.jiraBaseUrl || '';
  const connected = Boolean(state.connected);
  $('#jira-auth-dot').className = `integration-dot ${connected ? 'connected' : state.error ? 'failed' : ''}`;
  $('#jira-auth-title').textContent = connected ? (language === 'ru' ? 'Atlassian подключён' : 'Atlassian connected') : (language === 'ru' ? 'Требуется авторизация Atlassian' : 'Atlassian authorization required');
  $('#jira-auth-detail').textContent = state.error || (connected ? (language === 'ru' ? 'OAuth-сессия обновляется автоматически.' : 'The OAuth session refreshes automatically.') : (language === 'ru' ? 'Подключите Atlassian через браузер.' : 'Connect Atlassian in your browser.'));
  $('#jira-provider').textContent = state.provider || '—';
  $('#jira-configured').textContent = state.authentication === 'browser-oauth-2.1' ? 'Browser OAuth 2.1' : '—';
  $('#jira-sites').textContent = state.resources?.length ? state.resources.map(site => `${site.name} (${site.id})`).join(', ') : (language === 'ru' ? 'Нет' : 'None');
  $('#connect-jira').disabled = !state.configured || connected;
  $('#disconnect-jira').disabled = !connected;
  document.querySelector(`input[name="llm-provider"][value="${llm.provider}"]`).checked=true;
  $('#ollama-model').value=llm.ollamaModel||''; $('#external-llm-url').value=llm.external?.baseUrl||''; $('#external-llm-model').value=llm.external?.model||'';
  $('#external-llm-key').placeholder=llm.external?.configured?'Сохранён / configured':'••••••••';
}

document.querySelectorAll('.tabs > .tab').forEach(button => button.addEventListener('click', () => {
  activateView(button.dataset.view);
  if (button.dataset.view === 'logs') loadLogs();
  if (button.dataset.view === 'integration') loadJiraAuth();
}));
document.querySelectorAll('.document-mode').forEach(button => button.addEventListener('click', () => {
  viewerMode = button.dataset.mode;
  document.querySelectorAll('.document-mode').forEach(node => node.classList.toggle('active', node === button));
  if (activeProject) openProject(activeProject);
}));
$('#language-toggle').addEventListener('click', () => { language = language === 'ru' ? 'en' : 'ru'; localStorage.setItem('teamwork-language', language); applyLanguage(); if (sourceDocument) renderViewer(); });
$('#project-search').addEventListener('input', renderProjects);
$('#save-projects').addEventListener('click', async () => {
  if (!selectedProjects.length) { $('#projects-message').textContent = t('selectOne'); return; }
  $('#projects-message').textContent = language === 'ru' ? 'Сохранение и синхронизация…' : 'Saving and synchronizing…';
  const response = await fetch('/api/projects/selection', {method:'PUT',headers:{'content-type':'application/json'},body:JSON.stringify({projects:selectedProjects})});
  const result = await response.json();
  if (!response.ok) { $('#projects-message').textContent = result.error; return; }
  selectedProjects = result.selected;
  activeProject = selectedProjects.some(project => project.key === activeProject) ? activeProject : selectedProjects[0]?.key || '';
  $('#projects-message').textContent = result.sync?.lastError || t('selectionSaved');
  await load();
});
$('#sync').addEventListener('click', async () => { showStatus({running:true}); const state = await fetch('/api/sync',{method:'POST'}).then(r=>r.json()); showStatus(state); await load(); });
$('#save').addEventListener('click', async () => { try { if(!activeProject) throw Error(t('selectOne')); const grouping=JSON.parse($('#editor').value); const response=await fetch('/api/grouping',{method:'PUT',headers:{'content-type':'application/json'},body:JSON.stringify({project:activeProject,grouping})}); const result=await response.json(); if(!response.ok) throw Error(result.error); sourceConfig = {grouping: result.grouping}; $('#message').textContent=language === 'ru' ? `Правила ${activeProject} сохранены` : `${activeProject} rules saved`; showStatus(result.sync); } catch(error) { $('#message').textContent=`${language === 'ru' ? 'Ошибка' : 'Error'}: ${error.message}`; } });
$('#analyze-project').addEventListener('click', async () => { if(!activeProject) return; const button=$('#analyze-project'); button.disabled=true; button.textContent=language==='ru'?'Анализ…':'Analyzing…'; const response=await fetch(`/api/projects/${encodeURIComponent(activeProject)}/analyze`,{method:'POST'}); const result=await response.json(); button.disabled=false; button.textContent=t('analyzeProject'); if(!response.ok){showStatus({lastError:result.error});return;} sourceConfig={grouping:result.grouping}; $('#editor').value=JSON.stringify(result.grouping,null,2); $('#message').textContent=language==='ru'?`Добавлено правил: ${result.themes.length}`:`Rules added: ${result.themes.length}`; showStatus(result.sync); await openProject(activeProject); });
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
$('#refresh-jira-auth').addEventListener('click', loadJiraAuth);
$('#connect-jira').addEventListener('click', async () => {
  $('#jira-auth-message').textContent = 'Подготовка OAuth…';
  const response = await fetch('/api/jira-auth/start', {method: 'POST', headers: {'content-type':'application/json'}, body: JSON.stringify({jiraBaseUrl: $('#jira-base-url').value.trim()})});
  const result = await response.json();
  if (!response.ok) { $('#jira-auth-message').textContent = result.error; return; }
  location.href = result.url;
});
$('#save-jira-url').addEventListener('click', async () => {
  const response = await fetch('/api/workspace', {method:'PUT', headers:{'content-type':'application/json'}, body:JSON.stringify({jiraBaseUrl:$('#jira-base-url').value.trim()})});
  const result = await response.json();
  $('#jira-auth-message').textContent = response.ok ? (language === 'ru' ? 'Адрес Jira сохранён' : 'Jira address saved') : result.error;
});
$('#save-llm').addEventListener('click', async()=>{ const provider=document.querySelector('input[name="llm-provider"]:checked').value; const response=await fetch('/api/llm',{method:'PUT',headers:{'content-type':'application/json'},body:JSON.stringify({provider,ollamaModel:$('#ollama-model').value.trim(),external:{baseUrl:$('#external-llm-url').value.trim(),model:$('#external-llm-model').value.trim(),apiKey:$('#external-llm-key').value.trim()}})}); const result=await response.json(); $('#llm-status').textContent=response.ok?(language==='ru'?`Активный провайдер: ${result.provider}`:`Active provider: ${result.provider}`):result.error; if(response.ok) $('#external-llm-key').value=''; });
$('#disconnect-jira').addEventListener('click', async () => {
  if (!confirm('Удалить сохранённую авторизацию Atlassian?')) return;
  const response = await fetch('/api/jira-auth/disconnect', {method: 'DELETE'});
  const result = await response.json();
  $('#jira-auth-message').textContent = response.ok ? 'Авторизация Atlassian удалена' : result.error;
  await loadJiraAuth();
});
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

applyLanguage();
load();
if (location.hash === '#integration') {
  document.querySelector('.tab[data-view="integration"]')?.click();
  const result = new URLSearchParams(location.search).get('jiraAuth');
  if (result) $('#jira-auth-message').textContent = result === 'connected' ? 'Atlassian успешно подключён' : `Ошибка OAuth: ${result}`;
}
setInterval(() => fetch('/api/status').then(response => response.json()).then(showStatus), 5000);
