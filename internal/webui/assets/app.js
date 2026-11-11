'use strict';

/* ---------- 工具 ---------- */
const $ = (sel) => document.querySelector(sel);
const TYPES = { 1: '交付', 2: '测试', 3: '售后' };

/** 转义，所有插入 innerHTML 的用户数据都必须经过 */
function esc(s) {
  return String(s ?? '').replace(/[&<>"']/g, (c) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  })[c]);
}

function fmtDT(v) {
  if (!v) return '';
  const d = new Date(v);
  if (isNaN(d)) return '';
  const p = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

function todayStr() {
  const d = new Date();
  const p = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`;
}

function toast(msg, kind = '') {
  const el = document.createElement('div');
  el.className = 'toast ' + kind;
  el.textContent = msg;
  $('#toasts').appendChild(el);
  setTimeout(() => el.remove(), 2600);
}

async function api(path, opts = {}) {
  const res = await fetch(path, {
    headers: opts.body ? { 'Content-Type': 'application/json' } : undefined,
    ...opts,
  });
  if (!res.ok) {
    let msg = `请求失败 (${res.status})`;
    try {
      const j = await res.json();
      if (j && j.error) msg = j.error;
    } catch { /* 忽略非 JSON 响应 */ }
    throw new Error(msg);
  }
  if (res.status === 204) return null;
  return res.json();
}

/* ---------- 状态 ---------- */
const state = {
  items: [],
  selected: new Set(),
  sortKey: 'createdAt',
  sortDir: -1,
  editingId: null, // null = 新建
};

/* ---------- 统计 ---------- */
async function loadStats() {
  try {
    const s = await api('/api/stats');
    const cells = [
      ['工单总数', s.total, ''],
      ['未闭环', s.open, ''],
      ['已闭环', s.closed, 'ok'],
      ['已上升', s.rise, 'warn'],
      ['超时未处理', s.timeout, 'hot'],
      ['今日新增', s.today, ''],
      ['交付', s.delivery, ''],
      ['测试', s.test, ''],
      ['售后', s.afterSale, ''],
    ];
    $('#stats').innerHTML = cells.map(([k, v, cls]) =>
      `<div class="stat ${cls}"><div class="k">${k}</div><div class="v">${Number(v) || 0}</div></div>`
    ).join('');
  } catch (e) {
    console.error(e);
  }
}

/* ---------- 列表 ---------- */
function currentFilter() {
  return {
    q: $('#search').value.trim(),
    priority: $('#fPriority').value,
    status: $('#fStatus').value,
    hideClosed: $('#hideClosed').checked ? '1' : '',
  };
}

async function loadTickets(silent = false) {
  const f = currentFilter();
  const qs = new URLSearchParams({ q: f.q, priority: f.priority, status: f.status, hideClosed: f.hideClosed });
  try {
    const data = await api('/api/tickets?' + qs);
    state.items = data.items || [];
    // 清理已不存在的选中项
    const alive = new Set(state.items.map((t) => t.ticketId));
    [...state.selected].forEach((id) => { if (!alive.has(id)) state.selected.delete(id); });
    render();
    $('#statusText').textContent =
      `共 ${data.total} 条，当前显示 ${data.showing} 条` + (state.selected.size ? `，已选 ${state.selected.size} 条` : '');
    if (!silent) loadStats();
  } catch (e) {
    toast(e.message, 'err');
  }
}

function sorted() {
  const list = state.items.slice();
  const k = state.sortKey;
  const dir = state.sortDir;
  if (!k) return list;
  list.sort((a, b) => {
    let va = a[k], vb = b[k];
    if (k === 'createdAt' || k === 'updatedAt') {
      va = new Date(va).getTime() || 0;
      vb = new Date(vb).getTime() || 0;
    } else if (typeof va === 'number' && typeof vb === 'number') {
      return (va - vb) * dir;
    } else {
      va = String(va ?? '');
      vb = String(vb ?? '');
    }
    return va < vb ? -dir : va > vb ? dir : 0;
  });
  return list;
}

function statusClass(s) {
  return {
    '已闭环': 's-closed', '已上升': 's-rise',
    '处理中(超时)': 's-timeout', '新工单': 's-new', '处理中': 's-doing',
  }[s] || 's-doing';
}

function clip(text, n = 60) {
  const t = String(text ?? '');
  if (!t) return '<span class="blank">—</span>';
  const one = t.replace(/\s+/g, ' ').trim();
  return esc(one.length > n ? one.slice(0, n) + '…' : one);
}

/** 生成带省略号的单元格内容（外层 td.clip 负责裁剪） */
function clipCell(text) {
  const t = String(text ?? '').replace(/\s+/g, ' ').trim();
  return t ? `<span>${esc(t)}</span>` : '<span class="blank">—</span>';
}

function render() {
  const list = sorted();
  const tb = $('#tbody');
  if (!list.length) {
    tb.innerHTML = '';
    $('#empty').hidden = false;
    return;
  }
  $('#empty').hidden = true;

  const rows = list.map((t) => {
    const prio = Number(t.priority) || 3;
    const sel = state.selected.has(t.ticketId) ? ' selected' : '';
    const recLines = String(t.record || '').split('\n');
    const recPreview = recLines.slice(0, 3).join(' / ');
    return `<tr data-id="${esc(t.ticketId)}" class="${sel}">
      <td class="num"><input type="checkbox" class="rowchk" ${sel ? 'checked' : ''}></td>
      <td class="mono">${esc(t.ticketId)}</td>
      <td class="clip" title="${esc(t.clientName)}">${clipCell(t.clientName)}</td>
      <td class="clip" title="${esc(t.contactPerson || '')}">${clipCell(t.contactPerson)}</td>
      <td class="clip" title="${esc(t.contactPhone || '')}">${clipCell(t.contactPhone)}</td>
      <td class="clip" title="${esc(t.description)}">${clipCell(t.description)}</td>
      <td class="clip" title="${esc(t.record || '')}">${recPreview ? clipCell(recPreview) : '<span class="blank">—</span>'}</td>
      <td><span class="badge t${prio}">${esc(TYPES[prio] || '未知')}</span></td>
      <td class="num"><span class="toggle rise ${t.rise ? 'on' : ''}" data-act="rise">${t.rise ? '是' : '否'}</span></td>
      <td class="num"><span class="toggle ${t.closed ? 'on' : ''}" data-act="closed">${t.closed ? '是' : '否'}</span></td>
      <td class="datetime">${esc(fmtDT(t.createdAt))}</td>
      <td class="datetime">${esc(fmtDT(t.updatedAt))}</td>
      <td><span class="badge ${statusClass(t.status)}">${esc(t.status)}</span></td>
    </tr>`;
  });
  tb.innerHTML = rows.join('');

  // 排序表头箭头
  document.querySelectorAll('thead th[data-sort]').forEach((th) => {
    const on = th.dataset.sort === state.sortKey;
    th.classList.toggle('sorted', on);
    th.querySelector('.arrow')?.remove();
    if (on) {
      const s = document.createElement('span');
      s.className = 'arrow';
      s.textContent = state.sortDir > 0 ? '▲' : '▼';
      th.appendChild(s);
    }
  });

  const all = $('#checkAll');
  all.checked = list.length > 0 && list.every((t) => state.selected.has(t.ticketId));
  all.indeterminate = !all.checked && state.selected.size > 0;
}

/* ---------- 行内交互 ---------- */
function rowId(tr) { return tr.dataset.id; }

$('#tbody').addEventListener('click', async (e) => {
  const tr = e.target.closest('tr');
  if (!tr) return;
  const id = rowId(tr);

  const chk = e.target.closest('.rowchk');
  if (chk) {
    if (chk.checked) state.selected.add(id); else state.selected.delete(id);
    tr.classList.toggle('selected', chk.checked);
    updateSelUI();
    return;
  }

  const tg = e.target.closest('.toggle');
  if (tg) {
    const field = tg.dataset.act;
    const on = !tg.classList.contains('on');
    try {
      await api(`/api/tickets/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        body: JSON.stringify({ [field]: on }),
      });
      toast(`${field === 'closed' ? '闭环' : '上升'}状态已更新`, 'ok');
      await loadTickets();
    } catch (err) {
      toast(err.message, 'err');
    }
    return;
  }

  if (!e.ctrlKey && !e.metaKey) {
    // 单击选中该行
    state.selected = new Set([id]);
    updateSelUI();
  } else {
    if (state.selected.has(id)) state.selected.delete(id); else state.selected.add(id);
    updateSelUI();
  }
});

$('#tbody').addEventListener('dblclick', (e) => {
  const tr = e.target.closest('tr');
  if (!tr) return;
  const cell = e.target.closest('td');
  const idx = [...tr.children].indexOf(cell);
  // 处理记录列双击 → 记录编辑；其余 → 工单编辑
  if (idx === 6) openRecord(rowId(tr));
  else openTicketEdit(rowId(tr));
});

function updateSelUI() {
  document.querySelectorAll('#tbody tr').forEach((tr) => {
    const on = state.selected.has(rowId(tr));
    tr.classList.toggle('selected', on);
    const c = tr.querySelector('.rowchk');
    if (c) c.checked = on;
  });
  const list = state.items;
  const all = $('#checkAll');
  all.checked = list.length > 0 && list.every((t) => state.selected.has(t.ticketId));
  all.indeterminate = !all.checked && state.selected.size > 0;
  const total = $('#statusText').textContent.split('，已选')[0];
  $('#statusText').textContent = total + (state.selected.size ? `，已选 ${state.selected.size} 条` : '');
}

$('#checkAll').addEventListener('change', (e) => {
  if (e.target.checked) sorted().forEach((t) => state.selected.add(t.ticketId));
  else state.selected.clear();
  updateSelUI();
});

/* 表头排序 */
document.querySelectorAll('thead th[data-sort]').forEach((th) => {
  th.addEventListener('click', () => {
    const k = th.dataset.sort;
    if (state.sortKey === k) state.sortDir *= -1;
    else { state.sortKey = k; state.sortDir = 1; }
    render();
  });
});

/* ---------- 右键菜单 ---------- */
let ctx = null;
function closeCtx() { ctx?.remove(); ctx = null; }
document.addEventListener('click', closeCtx);
document.addEventListener('scroll', closeCtx, true);

$('#tbody').addEventListener('contextmenu', (e) => {
  e.preventDefault();
  const tr = e.target.closest('tr');
  if (!tr) return;
  const id = rowId(tr);
  if (!state.selected.has(id)) { state.selected = new Set([id]); updateSelUI(); }

  closeCtx();
  const m = document.createElement('div');
  m.className = 'ctxmenu';
  const item = (label, fn, cls = '') => {
    const b = document.createElement('button');
    b.textContent = label;
    b.className = cls;
    b.onclick = () => { closeCtx(); fn(id); };
    m.appendChild(b);
  };
  item('编辑工单', openTicketEdit);
  item('编辑处理记录', openRecord);
  item('标记为闭环', (i) => quickPatch(i, { closed: true }));
  item('标记为已上升', (i) => quickPatch(i, { rise: true }));
  m.appendChild(document.createElement('hr'));
  item('导出选中', () => doExport());
  item('删除', (i) => doDelete(), 'danger');
  document.body.appendChild(m);
  const { innerWidth: W, innerHeight: H } = window;
  const r = m.getBoundingClientRect();
  m.style.left = Math.min(e.clientX, W - r.width - 8) + 'px';
  m.style.top = Math.min(e.clientY, H - r.height - 8) + 'px';
  ctx = m;
});

async function quickPatch(id, patch) {
  try {
    await api(`/api/tickets/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify(patch) });
    toast('已更新', 'ok');
    await loadTickets();
  } catch (e) { toast(e.message, 'err'); }
}

/* ---------- 新建 / 编辑 ---------- */
const dlgTicket = $('#dlgTicket');

function showErr(el, msg) {
  if (!msg) { el.hidden = true; el.textContent = ''; return; }
  el.hidden = false;
  el.textContent = msg;
}

async function openTicketNew() {
  state.editingId = null;
  $('#dlgTitle').textContent = '新建工单';
  $('#fTicketId').value = '';
  $('#fTicketId').readOnly = false;
  $('#fClient').value = '';
  $('#fContact').value = '';
  $('#fPhone').value = '';
  $('#fDesc').value = '';
  $('#fRecord').value = '';
  $('#fRise').checked = false;
  $('#fClosed').checked = false;
  $('#fPriorityEdit').value = '3';
  $('#fCreated').value = todayStr();
  $('#idHint').hidden = false;
  showErr($('#dlgErr'), '');
  dlgTicket.showModal();
  await autoFillId();
  $('#fClient').focus();
}

async function openTicketEdit(id) {
  try {
    const t = await api(`/api/tickets/${encodeURIComponent(id)}`);
    state.editingId = t.ticketId;
    $('#dlgTitle').textContent = '编辑工单 · ' + t.ticketId;
    $('#fTicketId').value = t.ticketId;
    $('#fTicketId').readOnly = true;
    $('#fClient').value = t.clientName || '';
    $('#fContact').value = t.contactPerson || '';
    $('#fPhone').value = t.contactPhone || '';
    $('#fDesc').value = t.description || '';
    $('#fRecord').value = t.record || '';
    $('#fRise').checked = !!t.rise;
    $('#fClosed').checked = !!t.closed;
    $('#fPriorityEdit').value = String(t.priority || 3);
    $('#fCreated').value = (t.createdAt || '').slice(0, 10);
    $('#idHint').hidden = true;
    showErr($('#dlgErr'), '');
    dlgTicket.showModal();
  } catch (e) {
    toast(e.message, 'err');
  }
}

// 项目类型切换时自动带出单号（仅新建态）
async function autoFillId() {
  if (state.editingId) return;
  const prio = Number($('#fPriorityEdit').value);
  try {
    const r = await api(`/api/next-id?priority=${prio}`);
    const input = $('#fTicketId');
    if (r.ticketId) {
      input.value = r.ticketId;
      input.readOnly = true;
      $('#idHint').textContent = '已自动生成，可手动修改';
    } else {
      input.value = '';
      input.readOnly = false;
      $('#idHint').textContent = '售后工单需手工填写单号';
    }
  } catch (e) {
    console.error(e);
  }
}

$('#fPriorityEdit').addEventListener('change', autoFillId);
$('#fTicketId').addEventListener('input', (e) => { if (!e.target.readOnly) e.target.dataset.touched = '1'; });

$('#formTicket').addEventListener('submit', async (e) => {
  e.preventDefault();
  const payload = {
    ticketId: $('#fTicketId').value.trim(),
    clientName: $('#fClient').value.trim(),
    contactPerson: $('#fContact').value.trim(),
    contactPhone: $('#fPhone').value.trim(),
    description: $('#fDesc').value.trim(),
    record: $('#fRecord').value,
    rise: $('#fRise').checked,
    closed: $('#fClosed').checked,
    priority: Number($('#fPriorityEdit').value),
    createdDate: $('#fCreated').value || todayStr(),
  };

  const err = $('#dlgErr');
  if (!payload.ticketId) return showErr(err, '单号不能为空');
  if (!payload.clientName) return showErr(err, '客户名称不能为空');
  if (!payload.description) return showErr(err, '问题描述不能为空');

  try {
    if (state.editingId) {
      await api(`/api/tickets/${encodeURIComponent(state.editingId)}`, {
        method: 'PUT', body: JSON.stringify(payload),
      });
      toast('工单已更新', 'ok');
    } else {
      await api('/api/tickets', { method: 'POST', body: JSON.stringify(payload) });
      toast('工单已创建', 'ok');
      state.selected = new Set([payload.ticketId]);
    }
    dlgTicket.close();
    await loadTickets();
  } catch (ex) {
    showErr(err, ex.message);
  }
});

/* ---------- 处理记录 ---------- */
const dlgRecord = $('#dlgRecord');
let recId = null;

async function openRecord(id) {
  try {
    const t = await api(`/api/tickets/${encodeURIComponent(id)}`);
    recId = t.ticketId;
    $('#recTicket').textContent = `${t.ticketId} · ${t.clientName}`;
    $('#recText').value = t.record || '';
    dlgRecord.showModal();
    $('#recText').focus();
  } catch (e) { toast(e.message, 'err'); }
}

$('#btnCopyRec').addEventListener('click', async () => {
  const text = $('#recText').value;
  try {
    await navigator.clipboard.writeText(text);
    toast('已复制到剪贴板', 'ok');
  } catch {
    $('#recText').select();
    document.execCommand('copy');
    toast('已复制到剪贴板', 'ok');
  }
});

$('#formRecord').addEventListener('submit', async (e) => {
  e.preventDefault();
  if (!recId) return;
  try {
    await api(`/api/tickets/${encodeURIComponent(recId)}`, {
      method: 'PATCH', body: JSON.stringify({ record: $('#recText').value }),
    });
    dlgRecord.close();
    toast('处理记录已更新', 'ok');
    await loadTickets();
  } catch (ex) { toast(ex.message, 'err'); }
});

/* ---------- 删除 / 导出 ---------- */
async function doDelete() {
  const ids = [...state.selected];
  if (!ids.length) return toast('请先选择要删除的工单', 'err');
  if (!confirm(`确定删除选中的 ${ids.length} 个工单？此操作不可恢复！`)) return;
  try {
    const r = await api('/api/tickets', { method: 'DELETE', body: JSON.stringify({ ids }) });
    state.selected.clear();
    toast(`已删除 ${r.deleted} 个工单`, 'ok');
    await loadTickets();
  } catch (e) { toast(e.message, 'err'); }
}

function doExport() {
  const ids = state.selected.size
    ? [...state.selected]
    : sorted().map((t) => t.ticketId);
  if (!ids.length) return toast('没有可导出的工单', 'err');
  const fmt = confirm(`导出 ${ids.length} 条工单\n\n点击「确定」导出 CSV（可用 Excel 打开）\n点击「取消」导出 TXT（纯文本）`)
    ? 'csv' : 'txt';
  const url = `/api/export?format=${fmt}&ids=${encodeURIComponent(ids.join(','))}`;
  const a = document.createElement('a');
  a.href = url;
  a.download = '';
  document.body.appendChild(a);
  a.click();
  a.remove();
  toast(`已导出 ${ids.length} 条工单`, 'ok');
}

/* ---------- 事件绑定 ---------- */
$('#btnNew').addEventListener('click', openTicketNew);
$('#btnDelete').addEventListener('click', doDelete);
$('#btnExport').addEventListener('click', doExport);
$('#btnRefresh').addEventListener('click', () => { loadTickets(); toast('已刷新'); });

let searchTimer = null;
$('#search').addEventListener('input', () => {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(() => loadTickets(), 220);
});
$('#fPriority').addEventListener('change', () => loadTickets());
$('#fStatus').addEventListener('change', () => loadTickets());
$('#hideClosed').addEventListener('change', () => loadTickets());
$('#btnClear').addEventListener('click', () => {
  $('#search').value = '';
  $('#fPriority').value = '0';
  $('#fStatus').value = '0';
  $('#hideClosed').checked = false;
  state.selected.clear();
  loadTickets();
});

document.querySelectorAll('[data-close]').forEach((b) =>
  b.addEventListener('click', () => b.closest('dialog').close()));

// 快捷键
document.addEventListener('keydown', (e) => {
  const inField = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement.tagName);
  if (e.ctrlKey && e.key.toLowerCase() === 'n') { e.preventDefault(); openTicketNew(); return; }
  if (e.ctrlKey && e.key.toLowerCase() === 'e') { e.preventDefault(); doExport(); return; }
  if (e.key === 'F5') { e.preventDefault(); loadTickets(); return; }
  if (e.ctrlKey && e.key.toLowerCase() === 'f') { e.preventDefault(); $('#search').focus(); return; }
  if (e.key === 'Escape') closeCtx();
  if (!inField && (e.key === 'Delete') && state.selected.size) doDelete();
});

/* ---------- 启动 ---------- */
(async () => {
  try {
    const v = await api('/api/version');
    if (v && v.version) $('#verInfo').textContent = v.version;
  } catch { /* 版本信息非关键，忽略失败 */ }
  loadTickets();
})();
