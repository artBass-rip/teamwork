export function plainTaskText(title, url, body = '') {
  return [String(title || '').trim(), String(url || '').trim(), String(body || '').trim()].filter(Boolean).join('\n\n');
}

export async function writeRichClipboard(html, text) {
  if (navigator.clipboard?.write && globalThis.ClipboardItem) {
    const item = new ClipboardItem({
      'text/html': new Blob([html], {type: 'text/html'}),
      'text/plain': new Blob([text], {type: 'text/plain'})
    });
    await navigator.clipboard.write([item]);
    return;
  }
  const temporary = document.createElement('div');
  temporary.contentEditable = 'true';
  temporary.style.cssText = 'position:fixed;left:-10000px;top:0';
  temporary.innerHTML = html;
  document.body.append(temporary);
  const selection = getSelection();
  const range = document.createRange();
  range.selectNodeContents(temporary);
  selection.removeAllRanges();
  selection.addRange(range);
  const copied = document.execCommand('copy');
  selection.removeAllRanges();
  temporary.remove();
  if (!copied) throw new Error('Буфер обмена недоступен');
}
