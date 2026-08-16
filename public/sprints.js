const heading = line => /^(#{2,5})\s+(.+)$/.exec(line);
const withoutCount = value => value.replace(/\s+\(\d+\)$/, '');
const issueKey = block => block[0]?.match(/\b[A-Z][A-Z0-9_]*-\d+\b/)?.[0] || block[0];

export function groupMarkdownBySprint(markdown, config = {}) {
  const lines = String(markdown || '').replace(/\r/g, '').split('\n');
  const groups = new Map();
  let goal = '';
  let theme = '';
  let placement = '';
  let firstGroup = lines.findIndex(line => line.startsWith('## '));
  if (firstGroup < 0) firstGroup = lines.length;
  const preamble = lines.slice(1, firstGroup).join('\n').trim();

  for (let index = 0; index < lines.length; index += 1) {
    const match = heading(lines[index]);
    if (!match) continue;
    const level = match[1].length;
    const title = withoutCount(match[2]);
    if (level === 2) goal = title;
    if (level === 3) theme = title;
    if (level === 4) placement = title;
    if (level !== 5 || !placement) continue;
    let end = index + 1;
    while (end < lines.length) {
      const next = heading(lines[end]);
      if (next && next[1].length <= 5) break;
      end += 1;
    }
    const block = lines.slice(index, end);
    if (!groups.has(placement)) groups.set(placement, new Map());
    const goals = groups.get(placement);
    if (!goals.has(goal)) goals.set(goal, new Map());
    const themes = goals.get(goal);
    if (!themes.has(theme)) themes.set(theme, []);
    themes.get(theme).push(block);
    index = end - 1;
  }

  const labels = config?.grouping?.placementLabels || {};
  const category = name => name.startsWith(labels.active || 'Активный спринт') ? 0
    : name.startsWith(labels.future || 'Будущий спринт') ? 1
      : name.startsWith(labels.backlog || 'Бэклог') ? 2 : 3;
  const ordered = [...groups].sort(([a], [b]) => category(a) - category(b) || a.localeCompare(b, 'ru'));
  let output = '# Задачи по спринтам\n\n';
  if (preamble) output += `${preamble}\n\n`;
  for (const [sprint, goals] of ordered) {
    const sprintKeys = new Set([...goals.values()].flatMap(themes => [...themes.values()].flatMap(blocks => blocks.map(issueKey))));
    output += `## ${sprint} (${sprintKeys.size})\n\n`;
    for (const [goalName, themes] of [...goals].sort(([a], [b]) => a.localeCompare(b, 'ru'))) {
      const goalKeys = new Set([...themes.values()].flatMap(blocks => blocks.map(issueKey)));
      output += `### ${goalName} (${goalKeys.size})\n\n`;
      for (const [themeName, blocks] of [...themes].sort(([a], [b]) => a.localeCompare(b, 'ru'))) {
        output += `#### ${themeName} (${blocks.length})\n\n`;
        output += `${blocks.map(block => block.join('\n').trim()).join('\n\n')}\n\n`;
      }
    }
  }
  return output.trimEnd() + '\n';
}
