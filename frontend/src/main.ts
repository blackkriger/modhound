import './modhound.css';
import './app.css';

import {ApplyAppUpdate, Changelog, Check, CheckAppUpdate, ChoosePack, ChooseKey, ChooseServer, DefaultPack, KeepWindowSize, LastUpdate, LogFrontend, OpenLogFolder, OpenReport, OpenURL, SaveSettings, SelectPack, ServerBehind, SetConsoleOpen, SetSort, SetSkipped, Settings, Stop, SyncServer, Undo, Update} from '../wailsjs/go/main/App';
import {main, resolve} from '../wailsjs/go/models';
import {ClipboardSetText, EventsOn, Quit, WindowMinimise} from '../wailsjs/runtime/runtime';

type Mode = 'empty' | 'ready' | 'checking' | 'installing' | 'report';
type Kind = 'up' | 'upd' | 'need' | 'nf' | 'sk' | 'cur';
type InstallState = { state: string; percent: number; error: string };
type Row = { m: resolve.Mod; as?: string };

const STAGES: [string, string][] = [
    ['scan', 'Reading mods'],
    ['gtnh', 'Loading the GTNH catalog'],
    ['curseforge', 'Asking CurseForge'],
    ['modrinth', 'Asking Modrinth'],
    ['resolve', 'Looking for updates'],
];
const SOURCES: Record<string, string> = {gtnh: 'GTNH', curseforge: 'CurseForge', modrinth: 'Modrinth'};
const GLYPH: Record<string, [string, string]> = {
    update: ['↑', 'm3-g m3-g--up'],
    need: ['!', 'm3-g m3-g--bad'],
    nf: ['?', 'm3-g'],
    skipped: ['–', 'm3-g'],
    cur: ['=', 'm3-g'],
    done: ['✓', 'm3-g m3-g--ok'],
    downloading: ['↓', 'm3-g m3-g--now'],
    queued: ['·', 'm3-g'],
    failed: ['✕', 'm3-g m3-g--bad'],
};
const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

const icon = {
    chevron: '<svg class="i" viewBox="0 0 12 12"><path d="M3 4.5 6 7.5 9 4.5"></path></svg>',
    search: '<svg class="i" viewBox="0 0 16 16"><circle cx="7" cy="7" r="4.5"></circle><path d="M10.5 10.5 14 14"></path></svg>',
    gear: '<svg class="i" viewBox="0 0 16 16"><circle cx="8" cy="8" r="2"></circle><circle cx="8" cy="8" r="4.7"></circle><path d="M8 1.5v1.8M8 12.7v1.8M1.5 8h1.8M12.7 8h1.8M3.4 3.4l1.3 1.3M11.3 11.3l1.3 1.3M3.4 12.6l1.3-1.3M11.3 4.7l1.3-1.3"></path></svg>',
    min: '<svg class="i" viewBox="0 0 10 10"><path d="M0 5h10"></path></svg>',
    close: '<svg class="i" viewBox="0 0 10 10"><path d="M0 0l10 10M10 0 0 10"></path></svg>',
    eye: '<svg class="i" viewBox="0 0 16 16"><path d="M1.5 8S4 3.5 8 3.5 14.5 8 14.5 8 12 12.5 8 12.5 1.5 8 1.5 8z"></path><circle cx="8" cy="8" r="2"></circle></svg>',
    ext: '<svg class="i" viewBox="0 0 16 16"><path d="M6 3H3.5a.5.5 0 0 0-.5.5v9a.5.5 0 0 0 .5.5h9a.5.5 0 0 0 .5-.5V10M9 3h4v4M13 3 7.5 8.5"></path></svg>',
    folder: '<svg class="i" viewBox="0 0 16 16"><path d="M1.5 3.5h4.5l1.5 1.5h7v7.5h-13z"></path></svg>',
};

const state = {
    mode: 'empty' as Mode,
    root: '',
    pack: null as resolve.Pack | null,
    mods: new Map<string, resolve.Mod>(),
    settings: null as main.Settings | null,
    q: '',
    kindsOn: {up: true, upd: true, need: true, nf: true, sk: true, cur: false} as Record<Kind, boolean>,
    unchecked: new Set<string>(),
    selId: null as string | null,
    settingsOpen: false,
    showKey: false,
    draftKey: '',
    draftTheme: '',
    draftDebug: false,
    draftServer: '',
    draftServerKey: '',
    serverBehind: 0,
    syncing: false,
    serverEnter: false,
    recheck: false,
    logLines: [] as string[],
    keyboxDraft: '',
    pixelIcons: new Set<string>(),
    badIcons: new Set<string>(),
    sorted: null as resolve.Mod[] | null,
    firstSeen: new Map<string, number>(),
    lastUpdate: null as main.LastUpdate | null,
    notes: new Map<string, string>(),
    appUpdate: '',
    appUpdating: false,
    installing: 0,
    undoActive: [] as string[],
    notice: '',
    matched: null as Record<string, number> | null,
    stage: '',
    stageSeen: new Set<string>(),
    counts: {} as Record<string, [number, number]>,
    checkedAt: 0,
    installIds: [] as string[],
    install: new Map<string, InstallState>(),
    report: null as main.Report | null,
    error: '',
    doneAt: new Map<string, string>(),
    sort: {updates: 'date', updated: 'date', 'need you': 'date'} as Record<string, 'name' | 'date'>,
};

document.querySelector('#app')!.innerHTML = `
<div class="m3" id="root">
    <div class="m3-top" id="top">
        <span class="m3-mark" aria-hidden="true">modhound</span>
        <button type="button" class="m3-pack" id="pack"><span id="pack-name"></span> ${icon.chevron}</button>
        <span class="m3-meta" id="meta"></span>
        <label class="m3-search" id="search-box">
            ${icon.search}
            <input type="search" id="search" placeholder="Find a mod" aria-label="Find a mod" spellcheck="false">
        </label>
        <button type="button" class="m3-ghost" id="gear" aria-label="Settings">${icon.gear}</button>
        <div class="m3-caption">
            <button type="button" id="win-min" aria-label="Minimize">${icon.min}</button>
            <button type="button" id="win-close" class="is-close" aria-label="Close">${icon.close}</button>
        </div>
    </div>
    <div class="m3-body">
        <div class="m3-changes">
            <div class="m3-list" id="list" tabindex="-1"></div>
        </div>
        <aside class="m3-inspector" id="insp" aria-label="Details"></aside>
    </div>
    <section class="m3-console" id="console" hidden>
        <div class="m3-console-bar">
            <span class="m3-eyebrow">Debug log</span>
            <span class="m3-console-spacer"></span>
            <button type="button" class="m3-btn m3-btn--sm" id="console-copy">Copy</button>
            <button type="button" class="m3-btn m3-btn--sm" id="console-folder">Open log folder</button>
        </div>
        <pre class="m3-console-log" id="console-log"></pre>
    </section>
    <footer class="m3-foot">
        <div class="m3-status" id="status"></div>
        <div class="m3-actions">
            <button type="button" class="m3-btn" id="first"></button>
            <button type="button" class="m3-btn m3-btn--primary" id="primary"></button>
        </div>
    </footer>
</div>`;

const $ = <T extends HTMLElement = HTMLElement>(id: string) => document.getElementById(id) as T;

function morph(from: Node, to: Node) {
    if (from.nodeType !== to.nodeType || from.nodeName !== to.nodeName) {
        from.parentNode!.replaceChild(to, from);
        return;
    }
    if (!(from instanceof Element) || !(to instanceof Element)) {
        if (from.nodeValue !== to.nodeValue) from.nodeValue = to.nodeValue;
        return;
    }
    for (const {name} of Array.from(from.attributes)) if (!to.hasAttribute(name)) from.removeAttribute(name);
    for (const {name, value} of Array.from(to.attributes)) if (from.getAttribute(name) !== value) from.setAttribute(name, value);
    if (from instanceof HTMLInputElement && to instanceof HTMLInputElement) {
        if (from.checked !== to.checked) from.checked = to.checked;
        if (document.activeElement !== from && from.value !== to.value) from.value = to.value;
    }
    morphChildren(from, to);
}

function morphChildren(from: Element, to: Element) {
    const old = Array.from(from.childNodes);
    const next = Array.from(to.childNodes);
    next.forEach((n, i) => (i < old.length ? morph(old[i], n) : from.appendChild(n)));
    for (let i = next.length; i < old.length; i++) from.removeChild(old[i]);
}

function patch(el: HTMLElement, html: string) {
    const next = document.createElement(el.tagName);
    next.innerHTML = html;
    morphChildren(el, next);
}

function esc(s: unknown): string {
    return String(s ?? '').replace(/[&<>"']/g, c => ({'&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'}[c]!));
}

function loaderName(loader: string): string {
    return loader.charAt(0).toUpperCase() + loader.slice(1);
}

function plural(n: number, one: string, many: string): string {
    return n === 1 ? one : many;
}

function isNeed(m: resolve.Mod): boolean {
    return m.status === 'manual' || m.status === 'error';
}

function kindOf(m: resolve.Mod): Kind {
    if (m.skipped) return 'sk';
    if (m.status === 'update') return 'up';
    if (isNeed(m)) return 'need';
    if (m.status === 'current') return undoItem(m.key, m.fileName) ? 'upd' : 'cur';
    return 'nf';
}

function glyphOf(m: resolve.Mod): string {
    return {up: 'update', upd: 'done', need: 'need', nf: 'nf', sk: 'skipped', cur: 'cur'}[kindOf(m)];
}

const collator = new Intl.Collator(undefined, {sensitivity: 'base'});

function allMods(): resolve.Mod[] {
    if (!state.sorted) state.sorted = [...state.mods.values()].sort((a, b) => collator.compare(a.name, b.name));
    return state.sorted;
}

function modsChanged() {
    state.sorted = null;
}

function selectedUpdates(): resolve.Mod[] {
    return allMods().filter(m => kindOf(m) === 'up' && !state.unchecked.has(m.id));
}

function hasKey(): boolean {
    return !!state.settings?.curseforgeKey;
}

function matchesQuery(m: resolve.Mod): boolean {
    const q = state.q.trim().toLowerCase();
    return !q || m.name.toLowerCase().includes(q) || m.fileName.toLowerCase().includes(q);
}

function versionDiff(a: string, b: string): [string, string] {
    let i = 0;
    while (i < a.length && i < b.length && a[i] === b[i]) i++;
    while (i > 0 && /[0-9A-Za-z]/.test(b[i - 1])) i--;
    return [`${a} → ${b.slice(0, i)}`, b.slice(i)];
}

function shortError(text: string, source: string): string {
    const code = /HTTP (\d{3})/.exec(text)?.[1];
    if (code) return `${SOURCES[source] ?? 'HTTP'} ${code}`;
    if (/stopped/.test(text)) return 'stopped';
    if (/timeout|deadline|timed out/i.test(text)) return 'timed out';
    if (/hash/.test(text)) return 'bad download';
    if (/Java|class version/.test(text)) return 'newer Java';
    if (/game running/.test(text)) return 'file in use';
    return 'failed';
}

function releaseDate(d: string): string {
    const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(d);
    return m ? `released ${Number(m[3])} ${MONTHS[Number(m[2]) - 1]} ${m[1]}` : '';
}

function updatedTime(m: resolve.Mod): string {
    return state.doneAt.get(m.id) ?? undoItem(m.key, installedFile(m))?.time ?? '';
}

function nowStamp(): string {
    const d = new Date();
    const p = (n: number) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

function sortRows(label: string, rows: Row[]): Row[] {
    if (state.sort[label] !== 'date') return rows;
    const key = (m: resolve.Mod) => (label === 'updated' ? updatedTime(m) : m.target?.date ?? '');
    return [...rows].sort((a, b) => key(b.m).localeCompare(key(a.m)));
}

function updatedAt(t: string): string {
    const m = /^(\d{4})-(\d{2})-(\d{2}) (\d{2}:\d{2})/.exec(t);
    return m ? `updated ${Number(m[3])} ${MONTHS[Number(m[2]) - 1]} ${m[1]}, ${m[4]}` : '';
}

function ago(t: number): string {
    const min = Math.floor((Date.now() - t) / 60000);
    if (min < 1) return 'just now';
    if (min < 60) return `${min} min ago`;
    return `${Math.floor(min / 60)} h ago`;
}

function iconHTML(m: resolve.Mod, hero = false): string {
    const cls = hero ? 'm3-icon m3-hero-icon' : 'm3-icon';
    const letter = esc(m.name.charAt(0).toUpperCase());
    if (m.icon && !state.badIcons.has(m.icon)) return `<span class="${cls}"><img src="${esc(m.icon)}" alt=""${state.pixelIcons.has(m.icon) ? ' class="is-pixel"' : ''} data-letter="${letter}"></span>`;
    return `<span class="${cls}">${letter}</span>`;
}

function rowHTML({m, as}: Row): string {
    const st = as ?? glyphOf(m);
    const [g, gCls] = GLYPH[st];
    const inst = state.install.get(m.id);
    const showTo = !!m.target && ['update', 'need', 'done', 'downloading', 'queued', 'failed'].includes(st);
    const cur = m.version || m.fileName;
    const [v1, v2] = showTo ? versionDiff(cur, m.target!.version) : [cur, ''];
    let note = '';
    if (st === 'need') {
        note = m.status === 'error' ? `<span class="m3-note m3-note--bad">${esc(shortError(m.reason, m.source))}</span>` : '';
    } else if (st === 'failed') {
        const err = inst?.error || state.report?.failed?.find(f => f.id === m.id)?.error || '';
        note = `<span class="m3-note m3-note--bad">${esc(shortError(err, m.source))}</span>`;
    } else if (st === 'done') {
        const warning = state.mode === 'report' ? state.report?.installed?.find(f => f.id === m.id)?.warning : '';
        const when = updatedAt(updatedTime(m));
        if (warning) note = `<span class="m3-note m3-note--bad">${esc(warning)}</span>`;
        else if (when && state.sort.updated === 'date') note = `<span class="m3-note">${esc(when)}</span>`;
    } else if (st === 'queued') {
        note = '<span class="m3-note">queued</span>';
    } else if (st === 'downloading') {
        note = '<span class="m3-note is-busy">updating…</span>';
    }
    const section = st === 'update' ? 'updates' : st === 'need' ? 'need you' : '';
    if (!note && section && state.sort[section] === 'date' && m.target?.date) note = `<span class="m3-note">${esc(releaseDate(m.target.date))}</span>`;
    const busy = state.mode === 'checking' || state.mode === 'installing';
    const canCheck = st === 'update' || ['done', 'downloading', 'queued', 'failed'].includes(st) && state.mode === 'installing';
    const checked = state.mode === 'installing' || !state.unchecked.has(m.id);
    const selected = state.selId === m.id;
    const restoring = state.undoActive.includes(m.id);
    if (restoring) note = '<span class="m3-note is-busy">restoring…</span>';
    const open = state.mode !== 'checking' && !selected && !restoring;
    const canSkip = !busy && open && m.source !== '' && ['update', 'skipped', 'cur'].includes(st);
    const canUpdate = open && st === 'update' && !m.skipped;
    const canUndo = open && ['done', 'update', 'skipped'].includes(st) && !!undoItem(m.key, installedFile(m));
    const actions = [
        canUpdate ? '<button type="button" class="m3-act m3-act--primary" data-act="update">Update</button>' : '',
        canUndo ? '<button type="button" class="m3-act" data-act="undo">Undo</button>' : '',
        canSkip ? `<button type="button" class="m3-act" data-act="skip">${m.skipped ? 'Unskip' : 'Skip'}</button>` : '',
    ].join('');
    let enter = '';
    if (state.mode === 'checking') {
        const now = performance.now();
        const seen = state.firstSeen.get(m.id) ?? now;
        state.firstSeen.set(m.id, seen);
        if (now - seen < 320) enter = `--enter:-${Math.round(now - seen)}ms`;
    }
    const cls = ['m3-row', selected ? 'is-sel' : '', st === 'queued' ? 'is-dim' : '', actions ? 'has-acts' : '', enter ? 'is-new' : ''].filter(Boolean).join(' ');
    return `<div class="${cls}"${enter ? ` style="${enter}"` : ''} data-id="${esc(m.id)}">
        <span class="m3-check-slot">${canCheck ? `<input type="checkbox" class="m3-check" aria-label="Include in update" ${checked ? 'checked' : ''} ${busy ? 'disabled' : ''}>` : ''}</span>
        <button type="button" class="m3-row-main" aria-pressed="${selected}">
            <span class="${gCls}" aria-hidden="true">${g}</span>
            ${iconHTML(m)}
            <span class="m3-name">${esc(m.name)}</span>
            ${note}
            <span class="m3-ver">${esc(v1)}<b>${esc(v2)}</b></span>
        </button>
        ${actions ? `<span class="m3-acts">${actions}</span>` : ''}
    </div>`;
}

function renderChanges() {
    const list = $('list');
    const mods = allMods();
    const count = (k: Kind) => mods.filter(m => kindOf(m) === k).length;
    let sections: [string, Row[]][] = [];
    let tail = '';

    if (state.mode === 'empty') {
        list.innerHTML = '';
        return;
    }
    if (state.mode === 'checking') {
        sections = [
            ['updates', mods.filter(m => kindOf(m) === 'up').map(m => ({m}))],
            ['need you', mods.filter(m => kindOf(m) === 'need').map(m => ({m}))],
            ['not found', mods.filter(m => kindOf(m) === 'nf').map(m => ({m}))],
            ['skipped', mods.filter(m => kindOf(m) === 'sk').map(m => ({m}))],
        ];
        const total = state.counts.scan?.[1] ?? 0;
        const left = total - (state.counts.resolve?.[0] ?? 0);
        tail = total ? `${left} ${plural(left, 'mod', 'mods')} left to check` : '';
    } else if (state.mode === 'installing' || (state.mode === 'report' && state.report)) {
        const r = state.report;
        const live = state.mode === 'installing' ? state.install : new Map<string, InstallState>();
        const byId = (ids: string[]) => ids.map(id => state.mods.get(id)).filter(Boolean) as resolve.Mod[];
        const liveIds = [...live.keys()];
        const liveIn = (...st: string[]) => liveIds.filter(id => st.includes(live.get(id)!.state));
        const done = byId([...(r?.installed ?? []).map(f => f.id).filter(id => !live.has(id)), ...liveIn('done')]);
        const failed = byId([...(r?.failed ?? []).map(f => f.id).filter(id => !live.has(id)), ...liveIn('failed')]);
        const doneIds = new Set(done.map(m => m.id));
        const failedIds = new Set(failed.map(m => m.id));
        const rest = mods.filter(m => !doneIds.has(m.id) && !failedIds.has(m.id));
        const need = rest.filter(m => kindOf(m) === 'need');
        const left = rest.filter(m => kindOf(m) === 'up');
        const skipped = rest.filter(m => kindOf(m) === 'sk');
        const nf = rest.filter(m => kindOf(m) === 'nf');
        sections = [
            ['updates', left.map(m => ({m, as: live.get(m.id)?.state}))],
            ['failed', mods.filter(m => failedIds.has(m.id)).map(m => ({m, as: 'failed'}))],
            ['updated', mods.filter(m => doneIds.has(m.id) || (!failedIds.has(m.id) && kindOf(m) === 'upd')).map(m => ({m, as: 'done'}))],
            ['need you', need.map(m => ({m}))],
            ['not found', nf.map(m => ({m}))],
            ['skipped', skipped.map(m => ({m, as: 'skipped'}))],
        ];
    } else {
        const q = !!state.q.trim();
        const defs: [Kind, string, string, string][] = [
            ['up', '↑', 'm3-g--up', 'updates'],
            ['upd', '✓', 'm3-g--ok', 'updated'],
            ['need', '!', 'm3-g--bad', 'need you'],
            ['nf', '?', '', 'not found'],
            ['sk', '–', '', 'skipped'],
            ['cur', '=', '', 'current'],
        ];
        sections = defs.filter(([k]) => state.kindsOn[k] || q).map(([k, , , l]) => [l, mods.filter(m => kindOf(m) === k).map(m => ({m}))]);
        const labels: Record<Kind, [string, string]> = {up: ['update', 'updates'], upd: ['updated', 'updated'], need: ['need you', 'need you'], nf: ['not found', 'not found'], sk: ['skipped', 'skipped'], cur: ['current', 'current']};
        const hidden = defs.map(([k]) => k).filter(k => !state.kindsOn[k] && count(k) > 0).map(k => `${count(k)} ${plural(count(k), ...labels[k])}`);
        tail = !q && hidden.length ? `${hidden.join(', ')} hidden` : '';
    }

    let html = '';
    for (const [label, rows] of sections) {
        const visible = sortRows(label, rows.filter(r => matchesQuery(r.m)));
        if (!visible.length) continue;
        const selectable = state.mode === 'ready' && label === 'updates' && visible.some(r => kindOf(r.m) === 'up');
        const sort = state.mode !== 'checking' && label in state.sort && visible.length > 1
            ? `<button type="button" class="m3-sort" data-sort="${label}">by ${state.sort[label]}</button>`
            : '';
        html += selectable
            ? `<div class="m3-sep m3-sep--check"><span class="m3-check-slot"><input type="checkbox" class="m3-check" id="select-all" aria-label="Select all updates"></span>${label}${sort}</div>`
            : `<div class="m3-sep">${label}${sort}</div>`;
        html += visible.map(rowHTML).join('');
    }
    if (!html && state.mode === 'ready') html = `<div class="m3-empty">${state.q.trim() ? 'No mods match' : 'Nothing to show'}</div>`;
    if (tail) html += `<p class="m3-tail">${esc(tail)}</p>`;
    patch(list, html);
    syncSelectAll();
}

function visibleUpdates(): resolve.Mod[] {
    return allMods().filter(m => kindOf(m) === 'up' && matchesQuery(m));
}

function syncSelectAll() {
    const box = document.getElementById('select-all') as HTMLInputElement | null;
    if (!box) return;
    const mods = visibleUpdates();
    const on = mods.filter(m => !state.unchecked.has(m.id)).length;
    box.checked = on === mods.length;
    box.indeterminate = on > 0 && on < mods.length;
}

function serverLines(sync?: main.ServerSync): [string, string, number, string][] {
    const results = sync?.results ?? [];
    const ok = results.filter(x => x.ok).length;
    const failed = results.length - ok;
    return [
        ...(ok ? [['↑', '', ok, 'updated on the server'] as [string, string, number, string]] : []),
        ...(failed ? [['✕', 'm3-bad', failed, 'failed on the server'] as [string, string, number, string]] : []),
    ];
}

function linesHTML(items: [string, string, number, string][]): string {
    return `<ul class="m3-lines">${items.map(([g, gc, n, label]) => `<li><span class="g ${gc}">${g}</span><span class="${gc === 'm3-bad' ? 'm3-bad' : label === 'up to date' ? 'm' : ''}"><span class="m3-num">${n}</span> ${label}</span></li>`).join('')}</ul>`;
}

function reasonText(m: resolve.Mod): string {
    const src = SOURCES[m.source] ?? m.source;
    const mc = state.pack?.mcVersion ?? '';
    if (m.skipped) return 'Skipped. It is not updated until you unskip it.';
    switch (m.status) {
        case 'update':
            return [`A newer file for Minecraft ${mc} ${loaderName(state.pack?.loader ?? '')} is on ${src}.`, m.reason].filter(Boolean).join(' ');
        case 'current':
            if (m.reason) return m.reason + '.';
            return `The latest file on ${src} is installed.`;
        case 'unknown':
            if (!hasKey()) return 'No matching project on GTNH or Modrinth. Without a CurseForge API key, CurseForge is not checked.';
            if (!state.pack?.curseforgeOk) return 'No matching project on GTNH or Modrinth. CurseForge was unavailable during this check.';
            return 'No matching project on GTNH, CurseForge or Modrinth.';
    }
    return m.reason;
}

function stageDone(s: string): boolean {
    const c = state.counts[s];
    return !!c && c[1] > 0 && c[0] >= c[1];
}

function runningStage(): string {
    return STAGES.find(([s]) => state.counts[s] && !stageDone(s))?.[0] ?? state.stage;
}

function stepsHTML(finished: boolean): string {
    const resolving = !!state.counts.resolve;
    const steps = STAGES.map(([s, label]) => {
        const c = state.counts[s];
        const hits = state.matched?.[s];
        if (c && (finished || stageDone(s))) return `<li><span class="g is-ok">✓</span><span class="t">${label}</span><span class="n m3-num" data-count="step-${s}">${hits ?? (c[1] > 1 ? c[1] : '')}</span></li>`;
        if (c) return `<li><span class="g is-now">›</span><span class="t">${label}</span><span class="n m3-num">${c[1] > 1 ? `${c[0]}/${c[1]}` : ''}</span></li>`;
        if (finished || resolving) return `<li class="is-wait"><span class="g">–</span><span class="t">${label}</span></li>`;
        return `<li class="is-wait"><span class="g">·</span><span class="t">${label}</span></li>`;
    }).join('');
    return `<ul class="m3-steps${finished ? ' is-done' : ''}">${steps}</ul>`;
}

function notesHTML(id: string): string {
    const text = state.notes.get(id);
    if (text === undefined) {
        if (!state.notes.has(id)) loadNotes(id);
        return '<div class="m3-notes"><span class="m3-eyebrow">What\'s new</span><p class="m3-reason is-muted">Loading…</p></div>';
    }
    if (!text) return '';
    return `<div class="m3-notes"><span class="m3-eyebrow">What's new</span><pre class="m3-notes-text">${esc(text)}</pre></div>`;
}

async function loadNotes(id: string) {
    if (state.notes.has(id)) return;
    state.notes.set(id, undefined as unknown as string);
    let text = '';
    try {
        text = await Changelog(id);
    } catch {
        text = '';
    }
    state.notes.set(id, text);
    if (state.selId === id) renderInspector();
}

function techHTML(d: resolve.ModDebug): string {
    const rows: [string, string][] = [
        ['Path', d.path],
        ['SHA-1', d.sha1],
        ['Fingerprint', String(d.fingerprint)],
        ['Class version', String(d.classMajor)],
        ['GTNH', d.gtnh ?? ''],
        ['CurseForge', d.curseforge ?? ''],
        ['Modrinth', d.modrinth ?? ''],
    ];
    return `<dl class="m3-tech">${rows.filter(([, v]) => v).map(([k, v]) => `<dt>${k}</dt><dd>${esc(v)}</dd>`).join('')}</dl>`;
}

function inspectorHTML(): string {
    const mods = allMods();
    const count = (k: Kind) => mods.filter(m => kindOf(m) === k).length;
    const sel = state.selId ? state.mods.get(state.selId) : undefined;
    const idle = state.mode === 'ready' || state.mode === 'report';

    if (state.settingsOpen && (idle || state.mode === 'empty')) {
        return `<div class="m3-insp">
            <h2 class="m3-title">Settings</h2>
            <div class="m3-field">
                <label class="m3-label" for="cf-key">CurseForge API key</label>
                <div class="m3-input">
                    <input id="cf-key" type="${state.showKey ? 'text' : 'password'}" value="${esc(state.draftKey)}" placeholder="Paste the key" spellcheck="false" autocomplete="off">
                    <button type="button" id="key-eye" aria-label="${state.showKey ? 'Hide key' : 'Show key'}">${icon.eye}</button>
                </div>
            </div>
            <div class="m3-field">
                <label class="m3-label" for="srv-dir">Server</label>
                <div class="m3-input">
                    <input id="srv-dir" type="text" value="${esc(state.draftServer)}" placeholder="Folder or user@host:/path" spellcheck="false" autocomplete="off">
                    <button type="button" id="srv-browse" aria-label="Choose the server folder">${icon.folder}</button>
                </div>
            </div>
            <div class="m3-field${isRemote(state.draftServer) ? '' : ' is-off'}">
                <label class="m3-label" for="srv-key">SSH key</label>
                <div class="m3-input">
                    <input id="srv-key" type="text" value="${esc(state.draftServerKey)}" placeholder="Not set" spellcheck="false" autocomplete="off"${isRemote(state.draftServer) ? '' : ' disabled'}>
                    <button type="button" id="key-browse" aria-label="Choose the SSH key"${isRemote(state.draftServer) ? '' : ' disabled'}>${icon.folder}</button>
                </div>
                ${isRemote(state.draftServer) ? '' : `<p class="m3-hint">${state.draftServerKey ? 'Enter a remote server address in the server field to use this SSH key for it or select another one' : 'Enter a remote server address in the server field to select your SSH key for it'}</p>`}
            </div>
            <fieldset class="m3-field">
                <legend class="m3-label">Theme</legend>
                ${([['', 'System'], ['light', 'Light'], ['dark', 'Dark']] as [string, string][]).map(([v, label]) => `<label class="m3-radio"><input type="radio" name="theme" value="${v}" ${state.draftTheme === v ? 'checked' : ''}>${label}</label>`).join('')}
            </fieldset>
            <label class="m3-radio"><input type="checkbox" id="debug" ${state.draftDebug ? 'checked' : ''}>Debug mode</label>
            <div class="m3-last">
                <span class="m3-label m3-mono">modhound ${esc(state.settings?.version)} by blackkriger</span>
                ${state.appUpdate ? `<a href="#" class="m3-file${state.appUpdating ? ' is-busy' : busyOps() ? ' is-off' : ''}" id="app-update">${state.appUpdating ? 'Updating modhound…' : `Update to ${esc(state.appUpdate)}`}</a>` : ''}
            </div>
        </div>`;
    }

    if (sel && idle) {
        const t = sel.target;
        const canUpdate = sel.status === 'update' && !sel.skipped;
        const restore = undoItem(sel.key, installedFile(sel));
        const selUndo = state.undoActive.includes(sel.id);
        const showTo = !!t && (sel.status === 'update' || sel.status === 'manual' || state.mode === 'report');
        const links = [
            sel.url && sel.source ? `<a href="#" data-url="${esc(sel.url)}">Project page ${icon.ext}</a>` : '',
            showTo && t?.pageUrl ? `<a href="#" data-url="${esc(t.pageUrl)}">New version ${icon.ext}</a>` : '',
        ].filter(Boolean).join('');
        return `<div class="m3-insp">
            <div class="m3-hero">
                ${iconHTML(sel, true)}
                <div>
                    <h2 class="m3-title">${esc(sel.name)}</h2>
                    <div class="m3-by">${sel.authors?.length ? `by ${esc(sel.authors.join(', '))}` : 'Author not listed'}</div>
                </div>
            </div>
            ${sel.description ? `<p class="m3-desc">${esc(sel.description)}</p>` : ''}
            <div class="m3-diff">
                ${showTo
                    ? `<span class="minus">− ${esc(sel.fileName)}</span><span class="plus">+ ${esc(t!.fileName)}</span>${t!.date ? `<span class="when">${esc(releaseDate(t!.date))}</span>` : ''}`
                    : restore
                        ? `<span class="minus">− ${esc(restore.oldFile)}</span><span class="plus">+ ${esc(restore.newFile)}</span>${restore.time ? `<span class="when">${esc(updatedAt(restore.time))}</span>` : ''}`
                        : `<span>${esc(sel.fileName)}</span>`}
            </div>
            <p class="m3-reason">${esc(reasonText(sel))}</p>
            ${links ? `<div class="m3-links">${links}</div>` : ''}
            ${showTo && state.mode === 'ready' ? notesHTML(sel.id) : ''}
            ${sel.debug ? techHTML(sel.debug) : ''}
        </div>
        <div class="m3-insp-foot">
            ${canUpdate ? '<button type="button" class="m3-btn m3-btn--sm m3-btn--primary m3-btn--wide" id="sel-update">Update</button>' : ''}
            ${restore ? `<button type="button" class="m3-btn m3-btn--sm${canUpdate ? '' : ' m3-btn--wide'}" id="sel-restore" ${selUndo ? 'disabled' : ''}>${selUndo ? 'Restoring…' : 'Undo'}</button>` : ''}
            ${sel.source ? `<button type="button" class="m3-btn m3-btn--sm${canUpdate || restore ? '' : ' m3-btn--wide'}" id="sel-skip">${canUpdate || restore ? (sel.skipped ? 'Unskip' : 'Skip') : sel.skipped ? 'Unskip this mod' : 'Skip this mod'}</button>` : ''}
            <button type="button" class="m3-btn m3-btn--sm${sel.source ? '' : ' m3-btn--wide'}" id="sel-close">Close</button>
        </div>`;
    }

    if (state.mode === 'empty') {
        return '<div class="m3-insp"></div>';
    }

    if (state.mode === 'checking') {
        const total = state.counts.scan?.[1] ?? 0;
        const resolving = state.stage === 'resolve';
        const reading = state.stage === 'scan' || !state.stage;
        const big = resolving ? state.counts.resolve?.[0] ?? 0 : state.counts.scan?.[0] ?? 0;
        const label = resolving ? `of ${total} mods checked` : reading ? `of ${total} mods read` : 'mods read';
        return `<div class="m3-insp">
            <span class="m3-eyebrow">Checking</span>
            <div><p class="m3-big m3-num" data-count="${resolving ? 'checking' : 'reading'}">${big}</p><p class="m3-big-l">${label}</p></div>
            ${stepsHTML(false)}
        </div>`;
    }

    if (state.mode === 'installing') {
        const rows = state.installIds.map(id => [id, state.install.get(id)] as const);
        const log = rows.filter(([, s]) => s && s.state !== 'queued').map(([id, s]) => {
            const m = state.mods.get(id);
            const file = esc(m?.target?.fileName ?? m?.fileName ?? id);
            if (s!.state === 'done') return `<li><span class="g is-ok">✓</span><span class="t">${file}</span></li>`;
            if (s!.state === 'failed') return `<li><span class="g m3-bad">✕</span><span class="t">${file}</span><span class="n m3-bad">${esc(shortError(s!.error, m?.source ?? ''))}</span></li>`;
            return `<li><span class="g is-now">↓</span><span class="t">${file}</span><span class="n is-busy">updating…</span></li>`;
        }).join('');
        const queued = rows.filter(([, s]) => !s || s.state === 'queued').length;
        return `<div class="m3-insp">
            <span class="m3-eyebrow">Installing</span>
            <div><p class="m3-big m3-num" data-count="installing">${installDone()}</p><p class="m3-big-l">of ${state.installIds.length} ${plural(state.installIds.length, 'update', 'updates')}</p></div>
            <ul class="m3-steps">${log}${queued ? `<li class="is-wait"><span class="g">·</span><span class="t">${queued} more queued</span></li>` : ''}</ul>
        </div>`;
    }

    if (state.mode === 'report' && state.report) {
        const r = state.report;
        const n = r.installed?.length ?? 0;
        const f = r.failed?.length ?? 0;
        const file = r.path ? r.path.split(/[\\/]/).pop() : '';
        return `<div class="m3-insp">
            <span class="m3-eyebrow">Done</span>
            <div><p class="m3-big m3-num" data-count="report">${n}</p><p class="m3-big-l">${plural(n, 'mod updated', 'mods updated')}</p></div>
            ${linesHTML([
                ...(f ? [['✕', 'm3-bad', f, 'failed'] as [string, string, number, string]] : []),
                ...(count('up') ? [['↑', '', count('up'), plural(count('up'), 'update left', 'updates left')] as [string, string, number, string]] : []),
                ['!', 'm3-bad', r.manual?.length ?? 0, 'need you'],
                ['?', 'm', r.unknown?.length ?? 0, 'not found'],
                ['=', 'm', r.upToDate, 'up to date'],
                ...serverLines(r.server),
            ])}
            ${r.server?.error ? `<p class="m3-reason m3-bad">${esc(r.server.error)}</p>` : ''}
            ${r.server?.remote && r.server.results?.some(x => x.ok) ? '<p class="m3-reason">Restart the server to load the updated mods.</p>' : ''}
            ${n || f ? `<ul class="m3-steps">${[
                ...(r.failed ?? []).map(x => `<li><span class="g m3-bad">✕</span><span class="t">${esc(x.name)}</span><span class="n m3-bad">failed</span></li>`),
                ...[...(r.installed ?? [])].reverse().map(x => `<li><span class="g is-ok">✓</span><span class="t">${esc(x.name)}</span><span class="n">${esc(x.to)}</span></li>`),
            ].join('')}</ul>` : ''}
            ${file ? `<p class="m3-reason is-muted">Report saved as <a href="#" class="m3-file" id="open-report">${esc(file)}</a></p>` : ''}
        </div>
        <div class="m3-insp-foot">
            ${state.lastUpdate && n ? `<button type="button" class="m3-btn m3-btn--sm m3-btn--wide" id="undo-all" ${busyOps() ? 'disabled' : ''}>Undo all</button>` : ''}
            <button type="button" class="m3-btn m3-btn--sm${state.lastUpdate && n ? '' : ' m3-btn--wide'}" id="back" ${busyOps() ? 'disabled' : ''}>Back to the list</button>
        </div>`;
    }

    const up = count('up');
    const on = (k: Kind) => state.kindsOn[k] || !!state.q.trim();
    const line = (k: Kind, g: string, gc: string, label: string) => `<li><button type="button" class="m3-line" data-kind="${k}" aria-pressed="${on(k)}"><span class="g ${gc}">${g}</span><span class="${gc === 'm3-bad' ? 'm3-bad' : k === 'cur' ? 'm' : ''}"><span class="m3-num">${count(k)}</span> ${label}</span></button></li>`;
    return `<div class="m3-insp">
        <button type="button" class="m3-line m3-line--big" data-kind="up" aria-pressed="${on('up')}"><span class="m3-big m3-num" data-count="ready">${up}</span><span class="m3-big-l">${plural(up, 'update ready', 'updates ready')}</span></button>
        <div class="m3-split">
        <ul class="m3-lines">
            ${count('upd') ? line('upd', '✓', '', 'updated') : ''}
            ${line('need', '!', 'm3-bad', 'need you')}
            ${line('nf', '?', 'm', 'not found')}
            ${line('sk', '–', 'm', 'skipped')}
            ${line('cur', '=', 'm', 'current')}
        </ul>
        ${state.serverBehind ? `<div class="m3-last${state.serverEnter ? ' is-enter' : ''}">
            <span class="m3-eyebrow">Server</span>
            <p class="m3-reason"><span class="m3-num" data-count="server">${state.serverBehind}</span> ${plural(state.serverBehind, 'mod', 'mods')} behind the pack</p>
            <a href="#" class="m3-file${state.syncing ? ' is-busy' : busyOps() ? ' is-off' : ''}" id="sync-server">${state.syncing ? 'Syncing…' : 'Sync'}</a>
        </div>` : ''}
        </div>
        ${!hasKey() ? `<div class="m3-keybox">
            <span><b>No CurseForge API key.</b> Mods hosted only on CurseForge can't be checked.</span>
            <div class="m3-input"><input type="password" id="keybox-input" value="${esc(state.keyboxDraft)}" placeholder="Paste the key" aria-label="CurseForge API key" spellcheck="false" autocomplete="off"></div>
            <button type="button" class="m3-btn m3-btn--sm" id="keybox-save">Save and check again</button>
        </div>` : ''}
        ${(state.pack?.warnings ?? []).map(w => `<p class="m3-reason m3-bad">${esc(w)}</p>`).join('')}
        ${state.stageSeen.size ? stepsHTML(true) : ''}
    </div>`;
}

let inspView = '';

function renderInspector() {
    const insp = $('insp');
    const focused = document.activeElement?.id;
    const view = `${state.mode}|${state.selId ?? ''}|${state.settingsOpen}`;
    if (view === inspView) patch(insp, inspectorHTML());
    else insp.innerHTML = inspectorHTML();
    inspView = view;
    animateCounts(insp);
    if (focused === 'cf-key' || focused === 'keybox-input' || focused === 'srv-dir') $<HTMLInputElement>(focused)?.focus();
}

function renderTop() {
    const name = state.root ? state.root.split(/[\\/]/).filter(Boolean).pop() ?? state.root : 'Select folder';
    $('pack-name').textContent = name;
    const p = state.pack;
    $('meta').textContent = p ? [`Minecraft ${p.mcVersion}`, loaderName(p.loader), `${p.mods.length} mods`].join(', ') : '';
    $('gear').classList.toggle('is-on', state.settingsOpen);
    $('search-box').hidden = state.mode === 'empty';
    const busy = state.mode === 'checking' || state.mode === 'installing';
    $<HTMLButtonElement>('pack').disabled = busy || busyOps();
    $<HTMLButtonElement>('gear').disabled = busy || busyOps();
    $<HTMLButtonElement>('win-close').disabled = busyOps();
}

function updateLabel(n: number): string {
    return n && n < allMods().filter(m => kindOf(m) === 'up').length ? `Update ${n} selected` : 'Update all';
}

function renderFoot() {
    const status = $('status');
    const first = $<HTMLButtonElement>('first');
    const primary = $<HTMLButtonElement>('primary');
    switch (state.mode) {
        case 'empty':
            status.innerHTML = state.error ? `<span class="m3-bad">${esc(state.error)}</span>` : '<span>select the minecraft folder</span>';
            first.textContent = 'Check';
            first.disabled = !state.root;
            primary.textContent = 'Update all';
            primary.disabled = true;
            break;
        case 'checking': {
            const stage = runningStage();
            const [d, t] = state.counts[stage] ?? [0, 0];
            const label = STAGES.find(([s]) => s === stage)?.[1] ?? 'Checking';
            status.innerHTML = `<span><b>${esc(label.toLowerCase())}</b>${t > 1 ? ` ${d}/${t}` : ''}</span>`;
            first.textContent = 'Stop';
            first.disabled = false;
            primary.textContent = 'Update all';
            primary.disabled = true;
            break;
        }
        case 'installing': {
            const t = state.installIds.length;
            status.innerHTML = `<span><b>installing updates</b> ${installDone()}/${t}</span>`;
            first.textContent = 'Stop';
            first.disabled = false;
            primary.textContent = 'Updating…';
            primary.disabled = true;
            break;
        }
        case 'report': {
            const r = state.report!;
            const f = r.failed?.length ?? 0;
            status.innerHTML = state.error
                ? `<span class="m3-bad">${esc(state.error)}</span>`
                : `<span>finished, <b>${r.installed?.length ?? 0} updated</b>${f ? `, ${f} failed` : ''}</span>`;
            first.textContent = 'Check';
            first.disabled = false;
            const n = selectedUpdates().length;
            primary.textContent = f ? `Retry ${f} failed` : updateLabel(n);
            primary.disabled = busyOps() || (!f && n === 0);
            break;
        }
        default: {
            const n = selectedUpdates().length;
            status.innerHTML = state.error
                ? `<span class="m3-bad">${esc(state.error)}</span>`
                : state.notice
                    ? `<span>${esc(state.notice)}</span>`
                    : `<span>checked ${ago(state.checkedAt)}, <b>${state.pack?.mods.length ?? 0}</b> mods read</span>`;
            first.textContent = 'Check';
            first.disabled = false;
            primary.textContent = updateLabel(n);
            primary.disabled = busyOps() || n === 0;
        }
    }
}

function render() {
    renderTop();
    renderChanges();
    renderInspector();
    renderFoot();
}

let frame = 0;

function scheduleRender() {
    if (!frame) frame = window.setTimeout(() => {
        frame = 0;
        render();
    }, 120);
}

EventsOn('progress', (p: { stage: string; done: number; total: number }) => {
    if (state.mode === 'checking') {
        state.stage = p.stage;
        state.stageSeen.add(p.stage);
        state.counts[p.stage] = [p.done, p.total];
    }
    scheduleRender();
});

const MAX_LOG_LINES = 3000;

let consoleShown = false;

function renderConsole() {
    const debug = !!state.settings?.debug;
    if (consoleShown !== debug) {
        consoleShown = debug;
        SetConsoleOpen(debug);
    }
    $('console').hidden = !debug;
    if (debug) {
        const log = $('console-log');
        log.textContent = state.logLines.join('\n');
        log.scrollTop = log.scrollHeight;
    }
}

EventsOn('log', (lines: string[]) => {
    state.logLines.push(...lines);
    if (state.logLines.length > MAX_LOG_LINES) state.logLines.splice(0, state.logLines.length - MAX_LOG_LINES);
    if (!consoleShown) return;
    const log = $('console-log');
    const atBottom = log.scrollHeight - log.scrollTop - log.clientHeight < 24;
    log.textContent = state.logLines.join('\n');
    if (atBottom) log.scrollTop = log.scrollHeight;
});

window.addEventListener('resize', () => KeepWindowSize());

$('console-copy').addEventListener('click', () => ClipboardSetText(state.logLines.join('\n')));
$('console-folder').addEventListener('click', () => OpenLogFolder());

EventsOn('matched', (counts: Record<string, number>) => {
    if (state.mode !== 'checking') return;
    state.matched = counts;
    scheduleRender();
});

EventsOn('mod', (m: resolve.Mod) => {
    if (state.mode !== 'checking') return;
    state.mods.set(m.id, m);
    modsChanged();
    scheduleRender();
});

EventsOn('install', (e: { id: string; state: string; percent: number; error: string }) => {
    if (state.mode !== 'installing') return;
    state.install.set(e.id, {state: e.state, percent: e.percent, error: e.error});
    if (e.state === 'done') state.doneAt.set(e.id, nowStamp());
    scheduleRender();
});

async function runCheck() {
    if (!state.root || state.mode === 'checking' || state.mode === 'installing' || busyOps()) return;
    const previous = state.pack;
    const previousSteps = {stageSeen: state.stageSeen, counts: state.counts, matched: state.matched};
    state.mode = 'checking';
    state.error = '';
    state.notice = '';
    state.report = null;
    state.selId = null;
    state.settingsOpen = false;
    state.recheck = false;
    state.doneAt = new Map();
    state.mods = new Map();
    state.notes = new Map();
    modsChanged();
    state.stage = '';
    state.stageSeen = new Set();
    state.counts = {};
    state.matched = null;
    state.firstSeen = new Map();
    shown.set('checking', 0);
    shown.set('reading', 0);
    for (const [stage] of STAGES) shown.set(`step-${stage}`, 0);
    $('list').scrollTop = 0;
    render();
    try {
        const pack = await Check(state.root);
        state.pack = pack;
        state.mods = new Map(pack.mods.map(m => [m.id, m]));
        modsChanged();
        state.unchecked = new Set([...state.unchecked].filter(id => state.mods.has(id)));
        state.checkedAt = Date.now();
        state.lastUpdate = await LastUpdate().catch(() => null);
        state.mode = 'ready';
        reveal();
        refreshServer();
    } catch (e) {
        const err = String(e);
        if (err === 'stopped' && previous) {
            state.mods = new Map(previous.mods.map(m => [m.id, m]));
            state.stageSeen = previousSteps.stageSeen;
            state.counts = previousSteps.counts;
            state.matched = previousSteps.matched;
            state.mode = 'ready';
        } else {
            state.error = err === 'stopped' ? '' : err;
            state.pack = null;
            state.mods = new Map();
            state.mode = 'empty';
        }
        modsChanged();
    }
    render();
}

let revealTimer = 0;

function reveal() {
    const root = $('root');
    root.classList.remove('is-reveal');
    void root.offsetWidth;
    root.classList.add('is-reveal');
    clearTimeout(revealTimer);
    revealTimer = window.setTimeout(() => root.classList.remove('is-reveal'), 1200);
    shown.set('ready', 0);
}

function enterPanel() {
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) return;
    $('insp').animate([{opacity: 0, transform: 'translateY(6px)'}, {opacity: 1, transform: 'none'}], {duration: 400, easing: 'ease-out'});
}

const shown = new Map<string, number>();
const tweens = new Map<string, number>();

function animateCounts(scope: HTMLElement) {
    const still = matchMedia('(prefers-reduced-motion: reduce)').matches;
    scope.querySelectorAll<HTMLElement>('[data-count]').forEach(el => {
        const key = el.dataset.count!;
        const target = Number(el.textContent) || 0;
        const from = shown.get(key) ?? target;
        cancelAnimationFrame(tweens.get(key) ?? 0);
        if (still || from === target) {
            shown.set(key, target);
            return;
        }
        el.textContent = String(from);
        const begin = performance.now();
        const duration = Math.min(600, 150 + Math.abs(target - from) * 8);
        const step = (now: number) => {
            const t = Math.min(1, (now - begin) / duration);
            const value = Math.round(from + (target - from) * (1 - Math.pow(1 - t, 3)));
            shown.set(key, value);
            el.textContent = String(value);
            if (t < 1 && el.isConnected) tweens.set(key, requestAnimationFrame(step));
        };
        tweens.set(key, requestAnimationFrame(step));
    });
}

function installedFile(m: resolve.Mod): string {
    return state.mode === 'report' && state.report?.installed?.some(r => r.id === m.id) ? m.target?.fileName ?? '' : m.fileName;
}

function undoItem(key: string, file: string) {
    return state.lastUpdate?.restorable?.find(it => it.key === key && it.newFile === file);
}

function isRemote(s: string): boolean {
    s = s.trim();
    return s.startsWith('sftp://') || /^[^@\s/\\]+@[^:\s/\\]+:/.test(s);
}

function busyOps(): boolean {
    return state.installing > 0 || state.undoActive.length > 0;
}

function installDone(): number {
    return [...state.install.values()].filter(x => x.state === 'done' || x.state === 'failed').length;
}

async function runUndo(ids: string[], mods: string[]) {
    if (state.mode === 'checking' || state.mode === 'empty' || mods.some(id => state.undoActive.includes(id))) return;
    state.undoActive = [...state.undoActive, ...mods];
    render();
    try {
        const undo = await Undo(ids);
        const results = undo.results ?? [];
        const onServer = results.filter(r => r.ok && r.key.startsWith('server:')).length;
        const restored = results.filter(r => r.ok).length - onServer;
        const failed = results.filter(r => !r.ok).map(f => `${f.name} (${f.error})`);
        state.notice = `restored ${restored} ${plural(restored, 'mod', 'mods')}${onServer ? `, ${onServer} on the server` : ''}${failed.length ? `, ${failed.length} failed: ${failed.join(', ')}` : ''}`;
        const undone = new Set<string>();
        for (const {oldId, mod} of undo.mods ?? []) {
            if (!mod) continue;
            undone.add(oldId);
            state.mods.delete(oldId);
            state.mods.set(mod.id, mod);
            if (state.selId === oldId) state.selId = mod.id;
        }
        modsChanged();
        if (state.report) {
            state.report.installed = (state.report.installed ?? []).filter(r => !undone.has(r.id));
            if (state.mode === 'report' && !state.report.installed.length && !state.report.failed?.length) state.mode = 'ready';
        }
    } catch (e) {
        state.error = String(e);
    }
    state.lastUpdate = await LastUpdate().catch(() => null);
    state.undoActive = state.undoActive.filter(id => !mods.includes(id));
    render();
    if (!busyOps()) refreshServer();
}

async function refreshServer() {
    const behind = await ServerBehind().catch(() => 0);
    if (behind === state.serverBehind) return;
    if (!state.serverBehind) {
        shown.set('server', 0);
        state.serverEnter = true;
    }
    state.serverBehind = behind;
    if (state.mode === 'ready' && !state.selId && !state.settingsOpen) renderInspector();
    state.serverEnter = false;
}

async function runSync() {
    if (state.syncing || busyOps() || state.mode !== 'ready') return;
    state.syncing = true;
    renderInspector();
    try {
        const r = await SyncServer();
        const ok = (r.results ?? []).filter(x => x.ok).length;
        const failed = (r.results ?? []).filter(x => !x.ok);
        if (r.error) state.error = r.error;
        else state.notice = `synced ${ok} ${plural(ok, 'mod', 'mods')} to the server${r.remote && ok ? ', restart it to load them' : ''}${failed.length ? `, ${failed.length} failed: ${failed.map(f => `${f.name} (${f.error})`).join(', ')}` : ''}`;
    } catch (e) {
        state.error = String(e);
    }
    state.syncing = false;
    state.lastUpdate = await LastUpdate().catch(() => null);
    state.serverBehind = await ServerBehind().catch(() => 0);
    render();
}

async function runUpdate(ids: string[]) {
    if (state.mode !== 'ready' && state.mode !== 'report' && state.mode !== 'installing') return;
    ids = ids.filter(id => {
        const m = state.mods.get(id);
        return m && !m.skipped && m.status === 'update' && !state.install.has(id) && !state.undoActive.includes(id);
    });
    if (!ids.length) return;
    const continued = state.mode !== 'ready';
    if (state.mode !== 'installing') {
        state.error = '';
        if (!continued) state.report = null;
        state.mode = 'installing';
        state.selId = null;
        state.settingsOpen = false;
        state.installIds = [];
        state.install = new Map();
        shown.set('installing', 0);
        if (!continued) shown.set('report', 0);
        enterPanel();
    }
    state.installIds = [...state.installIds, ...ids];
    for (const id of ids) state.install.set(id, {state: 'queued', percent: 0, error: ''});
    state.installing++;
    render();
    try {
        state.report = await Update(ids, continued);
        for (const r of state.report.installed ?? []) {
            const m = state.mods.get(r.id);
            if (m) {
                m.status = 'current';
                m.reason = '';
            }
        }
    } catch (e) {
        state.error = String(e);
        for (const id of ids) state.install.delete(id);
        state.installIds = state.installIds.filter(id => !ids.includes(id));
    }
    state.installing--;
    if (state.installing > 0) {
        render();
        return;
    }
    state.lastUpdate = await LastUpdate().catch(() => null);
    state.mode = state.report ? 'report' : 'ready';
    state.install = new Map();
    render();
    enterPanel();
    refreshServer();
}

function closeSettings(): boolean {
    if (!state.settingsOpen) return false;
    state.settingsOpen = false;
    applyTheme();
    if (!state.recheck || !state.root || busyOps() || (state.mode !== 'ready' && state.mode !== 'report' && state.mode !== 'empty')) return false;
    runCheck();
    return true;
}

function openSettings(open: boolean) {
    if (state.mode === 'checking' || state.mode === 'installing') return;
    if (!open) {
        if (!closeSettings()) render();
        return;
    }
    state.settingsOpen = true;
    if (state.settings) {
        state.selId = null;
        state.draftKey = state.settings.curseforgeKey;
        state.draftTheme = state.settings.theme;
        state.draftDebug = state.settings.debug;
        state.draftServer = state.settings.server;
        state.draftServerKey = state.settings.serverKey;
        state.showKey = false;
    }
    applyTheme();
    render();
}

function applyTheme() {
    const theme = state.settingsOpen ? state.draftTheme : state.settings?.theme ?? '';
    if (theme) $('root').dataset.theme = theme;
    else delete $('root').dataset.theme;
}

async function saveSettings(key: string, theme: string, debug: boolean, server: string, serverKey: string) {
    const before = state.settings;
    try {
        await SaveSettings(key, theme, debug, server, serverKey);
        state.settings = await Settings();
    } catch (e) {
        state.error = String(e);
        renderFoot();
        return;
    }
    state.error = '';
    const changed = !before || before.curseforgeKey !== state.settings.curseforgeKey || before.debug !== state.settings.debug;
    if (changed) state.recheck = true;
    renderConsole();
    renderFoot();
    if (!state.settingsOpen) {
        state.keyboxDraft = '';
        render();
        if (changed && state.root) runCheck();
    }
    if (before?.server !== state.settings.server || before?.serverKey !== state.settings.serverKey) refreshServer();
}

function autosave() {
    saveSettings(state.draftKey, state.draftTheme, state.draftDebug, state.draftServer, state.draftServerKey);
}

async function toggleSkip(m: resolve.Mod) {
    const next = !m.skipped;
    const apply = (value: boolean) => {
        for (const x of state.mods.values()) {
            if (x.key === m.key) x.skipped = value;
        }
    };
    apply(next);
    render();
    try {
        await SetSkipped(m.key, next);
    } catch (e) {
        apply(!next);
        state.error = String(e);
        render();
    }
}

async function openPack(dir: string) {
    try {
        await SelectPack(dir);
    } catch (e) {
        state.error = String(e);
        render();
        return;
    }
    state.root = dir;
    state.pack = null;
    state.unchecked = new Set();
    runCheck();
}

async function choosePack() {
    try {
        const dir = await ChoosePack();
        if (!dir) return;
        state.root = dir;
        state.pack = null;
        state.unchecked = new Set();
        runCheck();
    } catch (e) {
        state.error = String(e);
        render();
    }
}

$('pack').addEventListener('click', choosePack);
$('gear').addEventListener('click', () => openSettings(!state.settingsOpen));
async function applyAppUpdate() {
    if (state.appUpdating || busyOps()) return;
    state.appUpdating = true;
    renderInspector();
    try {
        await ApplyAppUpdate();
    } catch (e) {
        state.error = String(e);
        state.appUpdating = false;
        render();
    }
}

$('win-min').addEventListener('click', () => WindowMinimise());
$('win-close').addEventListener('click', () => Quit());

$<HTMLInputElement>('search').addEventListener('input', e => {
    state.q = (e.target as HTMLInputElement).value;
    renderChanges();
    if (state.mode === 'ready' && !state.selId && !state.settingsOpen) renderInspector();
});

$('first').addEventListener('click', () => {
    if (state.mode === 'checking' || state.mode === 'installing') Stop();
    else if (!busyOps()) runCheck();
});

$('primary').addEventListener('click', () => {
    if (state.mode === 'ready') runUpdate(selectedUpdates().map(m => m.id));
    else if (state.mode === 'report') runUpdate(state.report?.failed?.length ? state.report.failed.map(f => f.id) : selectedUpdates().map(m => m.id));
});

$('list').addEventListener('click', e => {
    const el = e.target as HTMLElement;
    const sort = el.closest<HTMLElement>('[data-sort]')?.dataset.sort;
    if (sort) {
        state.sort[sort] = state.sort[sort] === 'date' ? 'name' : 'date';
        SetSort(sort, state.sort[sort]).catch(() => {});
        renderChanges();
        return;
    }
    if (el.id === 'select-all') {
        const mods = visibleUpdates();
        const all = mods.every(m => !state.unchecked.has(m.id));
        for (const m of mods) all ? state.unchecked.add(m.id) : state.unchecked.delete(m.id);
        renderChanges();
        renderFoot();
        return;
    }
    const row = el.closest<HTMLElement>('.m3-row');
    const m = row ? state.mods.get(row.dataset.id!) : undefined;
    if (!m) return;
    if (el.classList.contains('m3-check')) {
        (el as HTMLInputElement).checked ? state.unchecked.delete(m.id) : state.unchecked.add(m.id);
        syncSelectAll();
        renderFoot();
        return;
    }
    const act = el.closest<HTMLElement>('[data-act]')?.dataset.act;
    if (act === 'skip') {
        toggleSkip(m);
        return;
    }
    if (act === 'update') {
        runUpdate([m.id]);
        return;
    }
    if (act === 'undo') {
        runUndo([`${m.key}|${installedFile(m)}`], [m.id]);
        return;
    }
    if (el.closest('.m3-row-main') && (state.mode === 'ready' || state.mode === 'report')) {
        if (closeSettings()) return;
        state.selId = state.selId === m.id ? null : m.id;
        render();
        document.querySelector<HTMLElement>(`.m3-row[data-id="${CSS.escape(m.id)}"] .m3-row-main`)?.focus();
    }
});

$('list').addEventListener('keydown', e => {
    const current = (e.target as HTMLElement).closest<HTMLElement>('.m3-row-main');
    if (!current) return;
    const all = Array.from(document.querySelectorAll<HTMLElement>('.m3-row-main'));
    const i = all.indexOf(current);
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
        e.preventDefault();
        all[Math.max(0, Math.min(all.length - 1, i + (e.key === 'ArrowDown' ? 1 : -1)))]?.focus();
    } else if (e.key === ' ') {
        const check = current.parentElement?.querySelector<HTMLInputElement>('.m3-check');
        if (check && !check.disabled) {
            e.preventDefault();
            check.click();
        }
    }
});

$('insp').addEventListener('click', e => {
    const el = e.target as HTMLElement;
    const link = el.closest<HTMLElement>('[data-url]');
    if (link) {
        e.preventDefault();
        OpenURL(link.dataset.url!);
        return;
    }
    const kind = el.closest<HTMLElement>('[data-kind]')?.dataset.kind as Kind | undefined;
    if (kind && state.mode === 'ready' && !state.q.trim()) {
        state.kindsOn[kind] = !state.kindsOn[kind];
        renderChanges();
        renderInspector();
        return;
    }
    const id = el.closest<HTMLElement>('button, a')?.id;
    if (id === 'open-report' || id === 'app-update' || id === 'sync-server') e.preventDefault();
    const sel = state.selId ? state.mods.get(state.selId) : undefined;
    switch (id) {
        case 'key-eye':
            state.showKey = !state.showKey;
            renderInspector();
            break;
        case 'sel-skip':
            if (sel) toggleSkip(sel);
            break;
        case 'sel-update':
            if (sel) runUpdate([sel.id]);
            break;
        case 'sel-close':
            state.selId = null;
            render();
            break;
        case 'open-report':
            if (state.report?.path) OpenReport(state.report.path);
            break;
        case 'app-update':
            applyAppUpdate();
            break;
        case 'key-browse':
            ChooseKey().then(file => {
                if (!file) return;
                state.draftServerKey = file;
                renderInspector();
                autosave();
            }).catch(err => {
                state.error = String(err);
                renderFoot();
            });
            break;
        case 'srv-browse':
            ChooseServer().then(dir => {
                if (!dir) return;
                state.draftServer = dir;
                renderInspector();
                autosave();
            }).catch(err => {
                state.error = String(err);
                renderFoot();
            });
            break;
        case 'sync-server':
            runSync();
            break;
        case 'undo-all':
            runUndo([], (state.report?.installed ?? []).map(r => r.id));
            break;
        case 'sel-restore':
            if (sel) runUndo([`${sel.key}|${installedFile(sel)}`], [sel.id]);
            break;
        case 'back':
            runCheck();
            break;
        case 'keybox-save': {
            const key = state.keyboxDraft.trim();
            if (key) saveSettings(key, state.settings?.theme ?? '', state.settings?.debug ?? false, state.settings?.server ?? '', state.settings?.serverKey ?? '');
            break;
        }
    }
});

$('insp').addEventListener('input', e => {
    const el = e.target as HTMLInputElement;
    if (el.id === 'cf-key') state.draftKey = el.value;
    if (el.id === 'srv-dir') {
        state.draftServer = el.value;
        renderInspector();
    }
    if (el.id === 'srv-key') state.draftServerKey = el.value;
    if (el.id === 'keybox-input') state.keyboxDraft = el.value;
});

document.addEventListener('keydown', e => {
    const search = $<HTMLInputElement>('search');
    if (e.key === 'Escape') {
        if (document.activeElement === search && search.value) {
            search.value = '';
            state.q = '';
            renderChanges();
            renderInspector();
        } else if (state.settingsOpen || state.selId) {
            state.selId = null;
            if (!closeSettings()) render();
        }
    }
});

document.addEventListener('load', e => {
    const img = e.target as HTMLImageElement;
    if (img.tagName === 'IMG' && img.naturalWidth <= 64 && img.naturalHeight <= 64) {
        img.classList.add('is-pixel');
        state.pixelIcons.add(img.getAttribute('src') ?? '');
    }
}, true);

document.addEventListener('error', e => {
    const img = e.target as HTMLImageElement;
    if (img.tagName === 'IMG') {
        state.badIcons.add(img.getAttribute('src') ?? '');
        const box = img.parentElement;
        if (box) box.textContent = img.dataset.letter ?? '';
    }
}, true);

setInterval(() => {
    if (state.mode === 'ready') renderFoot();
}, 30000);

$('insp').addEventListener('change', e => {
    const el = e.target as HTMLInputElement;
    if (el.name === 'theme') {
        state.draftTheme = el.value;
        applyTheme();
        autosave();
    }
    if (el.id === 'debug') {
        state.draftDebug = el.checked;
        autosave();
    }
    if (el.id === 'cf-key' || el.id === 'srv-dir' || el.id === 'srv-key') autosave();
});

$('insp').addEventListener('auxclick', e => e.preventDefault());

window.addEventListener('error', e => {
    state.error = e.message;
    LogFrontend(`${e.message} at ${e.filename}:${e.lineno}:${e.colno}`);
    renderFoot();
});

window.addEventListener('unhandledrejection', e => {
    state.error = String(e.reason);
    LogFrontend(`unhandled rejection: ${e.reason}`);
    renderFoot();
});

(async () => {
    render();
    try {
        state.settings = await Settings();
    } catch (e) {
        state.error = String(e);
        render();
        return;
    }
    state.root = state.settings.lastPack;
    for (const [section, by] of Object.entries(state.settings.sort ?? {})) {
        if (section in state.sort && (by === 'name' || by === 'date')) state.sort[section] = by;
    }
    CheckAppUpdate().then(v => {
        state.appUpdate = v;
        if (state.settingsOpen) renderInspector();
    }).catch(() => {});
    applyTheme();
    renderConsole();
    if (state.root) {
        runCheck();
        return;
    }
    const found = await DefaultPack();
    if (found) openPack(found);
    else render();
})();
