(() => {
    "use strict";

    // ---- State ----
    let cfg = null;
    let state = { dryRun: false, debug: false, enclosure: '', jobs: [], activeJobs: [] };
    let selectedEnc = 0;
    let srcSlot = null;
    let dstSlot = null;
    let pendingOp = null; // { op, code }
    let pendingCancelId = null;
    let es = null;
    let reconnectTimer = null;
    let configPollTimer = null;
    const RECONNECT_DELAY = 3000;

    // ---- Init ----
    async function init() {
        try {
            const r = await fetch('/api/config');
            cfg = await r.json();
            selectedEnc = Number.isInteger(cfg.activeEnclosureIdx) ? cfg.activeEnclosureIdx : 0;
            if (cfg.needsSetup) {
                showSetupModal();
            } else {
                renderEnclosurePicker();
                renderAll();
            }
        } catch (e) {
            console.error('config load failed', e);
        }
        connectSSE();
        startConfigPolling();
    }

    // ---- SSE ----
    function connectSSE() {
        if (es) { es.close(); es = null; }
        if (reconnectTimer) { clearTimeout(reconnectTimer); reconnectTimer = null; }

        setConnBadge('reconnecting', 'Connecting...');
        es = new EventSource('/api/events');

        es.onopen = () => {
            setConnBadge('ok', 'Connected');
        };

        es.onmessage = (e) => {
            try {
                const s = JSON.parse(e.data);
                state = s;
                renderAll();
            } catch (_) { }
        };

        es.onerror = () => {
            setConnBadge('err', 'Disconnected');
            es.close(); es = null;
            reconnectTimer = setTimeout(connectSSE, RECONNECT_DELAY);
        };
    }

    function setConnBadge(cls, text) {
        const el = document.getElementById('conn-badge');
        el.className = 'badge-conn ' + cls;
        el.textContent = text;
    }

    function startConfigPolling() {
        if (configPollTimer) clearInterval(configPollTimer);
        configPollTimer = setInterval(async () => {
            try {
                const r = await fetch('/api/config');
                const newCfg = await r.json();
                // Check if deviceExists changed
                if (JSON.stringify(cfg.deviceExists) !== JSON.stringify(newCfg.deviceExists)) {
                    cfg.deviceExists = newCfg.deviceExists;
                    renderGrid();
                }
            } catch (_) { }
        }, 1000);
    }

    // ---- Render ----
    let isRendering = false;
    let renderScheduled = false;

    function renderAll() {
        if (isRendering) {
            if (!renderScheduled) {
                renderScheduled = true;
                requestAnimationFrame(() => {
                    renderScheduled = false;
                    renderAll();
                });
            }
            return;
        }

        isRendering = true;
        try {
            if (!cfg) return;
            document.getElementById('badge-dry').style.display = state.dryRun ? '' : 'none';
            document.getElementById('badge-debug').style.display = state.debug ? '' : 'none';
            if (state.dryRun) document.body.classList.add('dry-run-mode');
            else document.body.classList.remove('dry-run-mode');

            renderBanner();
            renderEnclosurePicker();
            renderGrid();
            renderOpPanel();
            renderJobList();
        } finally {
            isRendering = false;
        }
    }

    function renderBanner() {
        const badge = document.getElementById('enclosure-badge');
        badge.textContent = `Enclosure: ${currentEnclosureName()}`;
    }

    function renderEnclosurePicker() {
        const wrap = document.getElementById('picker-wrap');
        const picker = document.getElementById('enclosure-picker');
        if (!cfg || !picker || !wrap) return;
        wrap.classList.add('show');
        const options = cfg.enclosures.map((enclosure, index) => {
            const selected = index === selectedEnc ? ' selected' : '';
            return `<option value="${index}"${selected}>${enclosure.name}</option>`;
        }).join('');
        picker.innerHTML = options;
    }

    function activeJobsForPath(path) {
        return state.activeJobs.filter(j =>
            (j.state === 'pending' || j.state === 'running') && (j.src === path || j.dst === path)
        );
    }

    function renderGrid() {
        const enc = cfg.enclosures[selectedEnc];
        const grid = document.getElementById('disk-grid');
        grid.style.gridTemplateColumns = `repeat(${enc.cols}, var(--slot-w))`;
        grid.innerHTML = '';

        enc.grid.forEach(row => {
            row.forEach(slot => {
                const path = devicePath(enc, slot);
                const exists = !path || (cfg.deviceExists && cfg.deviceExists[path] !== false);
                const busy = isBusy(path);
                const isSrc = srcSlot === slot;
                const isDst = dstSlot === slot;

                let cls = 'disk-slot';
                if (!exists) cls += ' unavailable';
                if (isSrc) cls += ' selected-src';
                if (isDst) cls += ' selected-dst';
                if (busy) cls += ' busy';

                const div = document.createElement('div');
                div.className = cls;
                div.dataset.slot = slot;

                const usageLabel = getBusyLabel(path);
                const statusBadge = isSrc ? 'S' : isDst ? 'D' : (usageLabel || '');

                div.innerHTML = `
        ${statusBadge ? `<span class="slot-status">${statusBadge}</span>` : ''}
        <span class="slot-label">Slot${String(slot).padStart(2, '0')}</span>
      `;
                if (exists) {
                    div.addEventListener('click', () => onSlotClick(slot, path, busy));
                    div.addEventListener('contextmenu', (e) => { e.preventDefault(); showDiskInfo(slot); });
                }
                grid.appendChild(div);
            });
        });
    }

    function getBusyLabel(path) {
        let copyN = 0;
        for (const j of state.activeJobs) {
            const op = j.op || (j.src === j.dst ? 'erase' : 'copy');
            if (op === 'erase') {
                if (j.src === path || j.dst === path) return 'E';
            } else {
                copyN++;
                if (j.src === path) return `S${copyN}`;
                if (j.dst === path) return `D${copyN}`;
            }
        }
        return '';
    }

    function onSlotClick(slot, path, busy) {
        if (isRendering || busy) return;
        // unavailableスロットは弾く（念のため二重チェック）
        const enc = cfg.enclosures[selectedEnc];
        const exists = !path || (cfg.deviceExists && cfg.deviceExists[path] !== false);
        if (!exists) return;

        const prevSrc = srcSlot;
        const prevDst = dstSlot;

        if (srcSlot === null) {
            srcSlot = slot;
        } else if (srcSlot === slot) {
            srcSlot = null;
            dstSlot = null;
        } else {
            dstSlot = slot;
        }

        if (srcSlot !== prevSrc || dstSlot !== prevDst) {
            renderAll();
        }
    }

    function renderOpPanel() {
        const panel = document.getElementById('op-panel');
        if (srcSlot === null) { panel.style.display = 'none'; return; }
        panel.style.display = '';

        document.getElementById('op-src-val').textContent = `Slot${String(srcSlot).padStart(2, '0')}`;

        const dstStep = document.getElementById('op-dst-step');
        if (dstSlot !== null) {
            dstStep.style.display = '';
            document.getElementById('op-dst-val').textContent = `Slot${String(dstSlot).padStart(2, '0')}`;
        } else {
            dstStep.style.display = 'none';
        }

        document.getElementById('btn-copy').disabled = dstSlot === null;
    }

    function renderJobList() {
        const list = document.getElementById('job-list');
        const jobs = [...state.jobs].reverse();
        if (jobs.length === 0) {
            list.innerHTML = '<div style="color:var(--text-dim);font-size:0.85rem;">- No jobs -</div>';
            return;
        }
        list.innerHTML = '';
        jobs.forEach(j => {
            const card = document.createElement('div');
            card.className = `job-card state-${j.state}`;

            const op = j.op || 'copy';
            const srcLabel = slotLabel(j, j.src);
            const dstLabel = slotLabel(j, j.dst);
            const title = op === 'erase'
                ? `ERASE ${srcLabel}`
                : `COPY ${srcLabel} → ${dstLabel}`;

            const elapsed = formatElapsed(j.createdAt);
            const prog = j.progress || {};
            const pct = prog.percent || 0;
            const rate = prog.rate || '-';
            const remain = prog.remaining || '-';
            const rescued = prog.rescued || '-';
            const pass = prog.pass || 1;

            const isActive = j.state === 'running' || j.state === 'pending';

            card.innerHTML = `
      <span class="job-state">${j.state}</span>
      <div class="job-info">
        <div class="job-title">${title}</div>
        <div class="job-meta">${j.name} · ${j.id.slice(0, 8)} · ${elapsed}${j.errMsg ? ' · <span style="color:var(--red)">' + j.errMsg + '</span>' : ''}</div>
      </div>
      ${isActive ? `<div class="job-progress">
        ${op === 'erase' ? `
          <div class="progress-bar-wrap"><div class="progress-bar-fill" style="width:${pct.toFixed(1)}%"></div></div>
          <div class="progress-text">${pct.toFixed(1)}% · ${rate} · Written: ${rescued}</div>
        ` : `
          <div class="progress-bar-wrap"><div class="progress-bar-fill" style="width:${pct.toFixed(1)}%"></div></div>
          <div class="progress-text">Pass ${pass} · ${pct.toFixed(1)}% · ${rate} · Remain: ${remain}</div>
        `}
      </div>` : ''}
      ${isActive ? `<div class="job-actions"><button class="btn-danger" data-id="${j.id}">Cancel</button></div>` : ''}
    `;

            const cancelBtn = card.querySelector('[data-id]');
            if (cancelBtn) {
                cancelBtn.addEventListener('click', () => showCancelConfirm(j));
            }
            list.appendChild(card);
        });
    }

    // ---- Helpers ----
    function devicePath(enc, slot) {
        const key = String(slot);
        return (enc.devices && enc.devices[key]) || '';
    }

    function isBusy(path) {
        if (!path) return false;
        return state.activeJobs.some(j =>
            (j.state === 'pending' || j.state === 'running') && (j.src === path || j.dst === path)
        );
    }

    function shortDevice(path) {
        if (!path) return '-';
        const parts = path.split('/');
        return parts[parts.length - 1] || path;
    }

    function currentEnclosureName() {
        if (cfg && cfg.enclosures && cfg.enclosures[selectedEnc]) {
            return cfg.enclosures[selectedEnc].name;
        }
        return cfg && cfg.activeEnclosureName ? cfg.activeEnclosureName : '-';
    }

    function slotLabel(job, path) {
        const enc = cfg.enclosures.find(e => e.name === job.name);
        if (enc && enc.devices) {
            for (const [k, v] of Object.entries(enc.devices)) {
                if (v === path) return `Slot${String(k).padStart(2, '0')}`;
            }
        }
        const m = path.match(/(\d+)$/);
        return m ? `Slot${m[1].padStart(2, '0')}` : 'Slot??';
    }

    function formatElapsed(createdAt) {
        if (!createdAt) return '';
        const diff = Math.floor((Date.now() - new Date(createdAt).getTime()) / 1000);
        if (diff < 0) return '0s';
        const d = Math.floor(diff / 86400);
        const h = Math.floor((diff % 86400) / 3600);
        const m = Math.floor((diff % 3600) / 60);
        const s = diff % 60;
        if (d > 0) return `${d}d ${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
        return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
    }

    // ---- Operations ----
    document.getElementById('btn-copy').addEventListener('click', () => {
        if (srcSlot === null || dstSlot === null) return;
        showConfirm('copy', `COPY Slot${String(srcSlot).padStart(2, '0')} → Slot${String(dstSlot).padStart(2, '0')}`);
    });

    document.getElementById('btn-erase').addEventListener('click', () => {
        if (srcSlot === null) return;
        showConfirm('erase', `ERASE Slot${String(srcSlot).padStart(2, '0')}`);
    });

    document.getElementById('btn-info-op').addEventListener('click', () => {
        if (srcSlot !== null) showDiskInfo(srcSlot);
    });

    document.getElementById('btn-clear').addEventListener('click', () => {
        srcSlot = null; dstSlot = null;
        renderAll();
    });

    // ---- Confirm modal ----
    function showConfirm(op, actionText) {
        const code = genCode();
        pendingOp = { op, code };
        document.getElementById('confirm-title').textContent = op === 'erase' ? 'Confirm ERASE' : 'Confirm COPY';
        document.getElementById('confirm-action').textContent = actionText;
        document.getElementById('confirm-code-display').textContent = code;
        const inp = document.getElementById('confirm-input');
        inp.value = '';
        inp.classList.remove('mismatch');
        document.getElementById('confirm-overlay').classList.add('show');
        inp.focus();
    }

    function genCode() {
        return String(Math.floor(Math.random() * 10000)).padStart(4, '0');
    }

    document.getElementById('confirm-ok').addEventListener('click', doConfirm);
    document.getElementById('confirm-input').addEventListener('keydown', e => {
        if (e.key === 'Enter') doConfirm();
        if (e.key === 'Escape') closeConfirm();
    });
    document.getElementById('confirm-cancel').addEventListener('click', closeConfirm);

    async function doConfirm() {
        const inp = document.getElementById('confirm-input');
        if (!pendingOp) return;
        if (inp.value !== pendingOp.code) {
            inp.classList.add('mismatch');
            inp.focus();
            return;
        }
        const op = pendingOp.op;
        const body = {
            op,
            enclosureIdx: selectedEnc,
            srcSlot,
            dstSlot: op === 'copy' ? dstSlot : srcSlot,
        };
        closeConfirm();
        try {
            const r = await fetch('/api/jobs', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            });
            if (!r.ok) {
                const msg = await r.text();
                alert('Error: ' + msg);
                return;
            }
            srcSlot = null; dstSlot = null;
            renderAll();
        } catch (e) {
            alert('Network error: ' + e);
        }
    }

    function closeConfirm() {
        document.getElementById('confirm-overlay').classList.remove('show');
        pendingOp = null;
    }

    // ---- Cancel modal ----
    function showCancelConfirm(job) {
        pendingCancelId = job.id;
        const op = job.op || 'copy';
        const srcLabel = slotLabel(job, job.src);
        const dstLabel = slotLabel(job, job.dst);
        document.getElementById('cancel-job-line').textContent =
            op === 'erase' ? `ERASE ${srcLabel}` : `COPY ${srcLabel} → ${dstLabel}`;
        document.getElementById('cancel-overlay').classList.add('show');
    }

    document.getElementById('cancel-yes').addEventListener('click', async () => {
        document.getElementById('cancel-overlay').classList.remove('show');
        if (!pendingCancelId) return;
        try {
            await fetch(`/api/jobs/${pendingCancelId}`, { method: 'DELETE' });
        } catch (e) {
            console.error(e);
        }
        pendingCancelId = null;
    });
    document.getElementById('cancel-no').addEventListener('click', () => {
        document.getElementById('cancel-overlay').classList.remove('show');
        pendingCancelId = null;
    });

    // ---- Disk info modal ----
    async function showDiskInfo(slot) {
        const table = document.getElementById('info-table');
        table.innerHTML = '<tr><td colspan="2" style="color:var(--text-dim)">Loading...</td></tr>';
        document.getElementById('info-overlay').classList.add('show');
        try {
            const r = await fetch(`/api/diskinfo?enc=${selectedEnc}&slot=${slot}`);
            const info = await r.json();
            table.innerHTML = `
      <tr><td>Device</td><td>${info.device}</td></tr>
      <tr><td>Slot</td><td>${info.slot}</td></tr>
      <tr><td>Model</td><td>${info.model}</td></tr>
      <tr><td>Serial</td><td>${info.serial}</td></tr>
      <tr><td>Size</td><td>${info.size}</td></tr>
    `;
        } catch (e) {
            table.innerHTML = `<tr><td colspan="2" style="color:var(--red)">Failed to load: ${e}</td></tr>`;
        }
    }
    document.getElementById('info-close').addEventListener('click', () => {
        document.getElementById('info-overlay').classList.remove('show');
    });

    // Click outside modal to close
    ['confirm-overlay', 'cancel-overlay', 'info-overlay'].forEach(id => {
        document.getElementById(id).addEventListener('click', (e) => {
            if (e.target.id === id) e.target.classList.remove('show');
        });
    });

    // ---- Job list auto-refresh elapsed ----
    setInterval(() => {
        if (state.activeJobs.length > 0) renderJobList();
    }, 1000);

    // ---- Enclosure picker change ----
    document.getElementById('enclosure-picker').addEventListener('change', async (e) => {
        if (isRendering) return;
        const newEnc = Number(e.target.value);
        if (newEnc === selectedEnc) return;
        selectedEnc = newEnc;
        srcSlot = null;
        dstSlot = null;
        try {
            await fetch('/api/enclosure', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name: cfg.enclosures[newEnc].name }),
            });
        } catch (_) { }
        renderAll();
    });

    // ---- Setup modal (first-time selection) ----
    function showSetupModal() {
        const picker = document.getElementById('setup-picker');
        picker.innerHTML = (cfg.enclosures || []).map((e, i) =>
            `<option value="${i}">${e.name}</option>`
        ).join('');
        const overlay = document.getElementById('setup-overlay');
        overlay.style.display = '';
        overlay.classList.add('show');
    }

    document.getElementById('setup-ok').addEventListener('click', async () => {
        const picker = document.getElementById('setup-picker');
        const idx = Number(picker.value);
        const name = cfg.enclosures[idx].name;
        try {
            const r = await fetch('/api/enclosure', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ name }),
            });
            if (!r.ok) { alert('Error: ' + await r.text()); return; }
        } catch (err) { alert('Network error: ' + err); return; }
        selectedEnc = idx;
        cfg.activeEnclosureIdx = idx;
        cfg.activeEnclosureName = name;
        cfg.needsSetup = false;
        const overlay = document.getElementById('setup-overlay');
        overlay.classList.remove('show');
        renderEnclosurePicker();
        renderAll();
    });

    init();
})();
