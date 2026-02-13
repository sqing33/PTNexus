const SYNC_REPORT_KEY = "ptnexus_cookie_sync_last_report";

const STATUS = {
  all: { key: "all", label: "全部" },
  failed: { key: "failed", label: "失败" },
  success: { key: "success", label: "成功" },
  skipped: { key: "skipped", label: "跳过" },
  unknown: { key: "unknown", label: "未知" },
};

const STATUS_ORDER = {
  failed: 0,
  unknown: 1,
  success: 2,
  skipped: 3,
};

const els = {
  meta: null,
  summary: null,
  filters: null,
  tbody: null,
  empty: null,
  detailsPre: null,
  copyBtn: null,
};

let currentReport = null;
let currentResults = [];
let currentFilter = "all";
let selectedIndex = -1;

document.addEventListener("DOMContentLoaded", async () => {
  bindElements();
  bindEvents();
  await loadReport();
});

function bindElements() {
  els.meta = document.getElementById("meta");
  els.summary = document.getElementById("summary");
  els.filters = document.getElementById("filters");
  els.tbody = document.getElementById("tbody");
  els.empty = document.getElementById("empty");
  els.detailsPre = document.getElementById("detailsPre");
  els.copyBtn = document.getElementById("copyBtn");
}

function bindEvents() {
  window.addEventListener("hashchange", () => {
    applyFilterFromHash();
    render();
  });

  els.copyBtn.addEventListener("click", async () => {
    const text = els.detailsPre.textContent || "";
    if (!text.trim()) {
      return;
    }
    try {
      await navigator.clipboard.writeText(text);
      els.copyBtn.textContent = "已复制";
      setTimeout(() => {
        els.copyBtn.textContent = "复制 JSON";
      }, 900);
    } catch (_err) {
      els.copyBtn.textContent = "复制失败";
      setTimeout(() => {
        els.copyBtn.textContent = "复制 JSON";
      }, 900);
    }
  });
}

async function loadReport() {
  const data = await storageGet(SYNC_REPORT_KEY);
  currentReport = data[SYNC_REPORT_KEY] || null;

  applyFilterFromHash();

  if (!currentReport) {
    currentResults = [];
    els.meta.textContent = "";
    els.summary.innerHTML = "";
    els.filters.innerHTML = "";
    els.empty.hidden = false;
    els.empty.textContent = "暂无同步结果，请先在插件中执行“测试并同步”。";
    els.tbody.innerHTML = "";
    els.detailsPre.textContent = "点击左侧任意站点查看详情。";
    return;
  }

  const results = Array.isArray(currentReport.results) ? currentReport.results : [];
  currentResults = results.map((item, index) => normalizeResult(item, index));
  currentResults.sort(sortResults);
  render();
}

function applyFilterFromHash() {
  const raw = (location.hash || "").replace(/^#/, "").trim();
  if (!raw) {
    currentFilter = "all";
    return;
  }
  if (raw === "failed" || raw === "success" || raw === "skipped" || raw === "unknown") {
    currentFilter = raw;
    return;
  }
  currentFilter = "all";
}

function render() {
  renderMeta();
  renderSummary();
  renderFilters();
  renderTable();
}

function renderMeta() {
  const time = toSafeString(currentReport?.generated_at);
  const api = toSafeString(currentReport?.api_base_url);
  const user = toSafeString(currentReport?.username);
  const parts = [];
  if (api) parts.push(`API：${api}`);
  if (user) parts.push(`用户：${user}`);
  if (time) parts.push(`时间：${formatTime(time)}`);
  els.meta.textContent = parts.join(" · ");
}

function renderSummary() {
  const total = Number(currentReport?.total_targets || 0);
  const updated = Number(currentReport?.updated_count || 0);
  const skipped = Number(currentReport?.skipped_count || 0);
  const failed = Number(currentReport?.failed_count || 0);
  const message = toSafeString(currentReport?.message);

  els.summary.innerHTML = "";
}

function renderFilters() {
  const counts = countByStatus(currentResults);
  const items = [STATUS.all, STATUS.failed, STATUS.success, STATUS.skipped, STATUS.unknown];

  els.filters.innerHTML = items
    .map((s) => {
      const count = s.key === "all" ? currentResults.length : counts[s.key] || 0;
      const active = currentFilter === s.key ? " active" : "";
      return `<button type="button" class="filter-btn${active}" data-filter="${s.key}">${escapeHtml(
        s.label,
      )} (${count})</button>`;
    })
    .join("");

  const buttons = Array.from(els.filters.querySelectorAll("button[data-filter]"));
  for (const btn of buttons) {
    btn.addEventListener("click", () => {
      const key = btn.getAttribute("data-filter");
      if (!key) return;
      if (key === "all") {
        location.hash = "";
      } else {
        location.hash = `#${key}`;
      }
    });
  }
}

function renderTable() {
  const filtered = currentFilter === "all" ? currentResults : currentResults.filter((r) => r.status === currentFilter);

  els.tbody.innerHTML = filtered
    .map((r, i) => {
      const selected = selectedIndex === r._index ? " selected" : "";
      return `<tr class="${selected}" data-index="${r._index}">
        <td><span class="badge ${r.status}">${escapeHtml(statusLabel(r.status))}</span></td>
        <td>${escapeHtml(r.site)}</td>
        <td>${escapeHtml(r.nickname)}</td>
        <td class="url">${renderUrlCell(r.url)}</td>
        <td>${escapeHtml(r.message)}</td>
      </tr>`;
    })
    .join("");

  const rows = Array.from(els.tbody.querySelectorAll("tr[data-index]"));
  for (const row of rows) {
    row.addEventListener("click", () => {
      const rawIndex = row.getAttribute("data-index");
      const idx = Number(rawIndex);
      if (!Number.isFinite(idx)) return;
      selectedIndex = idx;
      const record = currentResults.find((r) => r._index === idx);
      if (record) {
        els.detailsPre.textContent = JSON.stringify(record.raw, null, 2);
      }
      renderTable();
    });
  }

  if (!filtered.length) {
    els.empty.hidden = false;
    els.empty.textContent = "该筛选条件下没有结果。";
  } else {
    els.empty.hidden = true;
  }
}

function normalizeResult(item, index) {
  const raw = item && typeof item === "object" ? item : { value: item };
  const site = toSafeString(raw.site || raw.name || raw.host || raw.domain);
  const nickname = toSafeString(raw.nickname || raw.title);
  const url = normalizeUrl(raw);

  const status = normalizeStatus(raw);
  const message = toSafeString(raw.message || raw.error || raw.reason || raw.detail || raw.status_message);

  return {
    _index: index,
    site,
    nickname,
    url,
    status,
    message: message || "-",
    raw,
  };
}

function normalizeStatus(raw) {
  const msg = toSafeString(raw.message || raw.detail || raw.reason);
  if (msg.includes("未更新") || msg.includes("无需更新") || msg.includes("不需要更新") || msg.includes("未命中站点")) {
    return "success";
  }

  if (raw.success === true) return "success";
  if (raw.success === false) return "failed";

  if (raw.updated === false || raw.is_updated === false) return "success";

  const s = toSafeString(raw.status || raw.state || raw.result).toLowerCase();
  if (!s) return "unknown";

  if (
    ["ok", "success", "succeeded", "passed", "updated", "done", "unchanged", "nochange", "no_change", "not_updated"].includes(
      s,
    )
  )
    return "success";
  if (["fail", "failed", "error", "denied", "rejected"].includes(s)) return "failed";
  if (["skip", "skipped", "ignored", "empty"].includes(s)) return "skipped";

  if (s.includes("fail") || s.includes("error")) return "failed";
  if (s.includes("skip")) return "skipped";
  if (s.includes("not updated") || s.includes("not_updated") || s.includes("unchanged") || s.includes("no change")) return "success";
  if (s.includes("success") || s.includes("ok")) return "success";

  return "unknown";
}

function sortResults(a, b) {
  const aOrder = STATUS_ORDER[a.status] ?? 99;
  const bOrder = STATUS_ORDER[b.status] ?? 99;
  if (aOrder !== bOrder) return aOrder - bOrder;
  if (a.site !== b.site) return a.site.localeCompare(b.site);
  if (a.nickname !== b.nickname) return a.nickname.localeCompare(b.nickname);
  return a._index - b._index;
}

function countByStatus(results) {
  const counts = { failed: 0, success: 0, skipped: 0, unknown: 0 };
  for (const r of results) {
    if (counts[r.status] === undefined) continue;
    counts[r.status] += 1;
  }
  return counts;
}

function statusLabel(status) {
  switch (status) {
    case "failed":
      return STATUS.failed.label;
    case "success":
      return STATUS.success.label;
    case "skipped":
      return STATUS.skipped.label;
    default:
      return STATUS.unknown.label;
  }
}

function storageGet(keys) {
  return new Promise((resolve, reject) => {
    chrome.storage.local.get(keys, (result) => {
      if (chrome.runtime.lastError) {
        reject(new Error(chrome.runtime.lastError.message));
        return;
      }
      resolve(result || {});
    });
  });
}

function toSafeString(value) {
  if (value === null || value === undefined) {
    return "";
  }
  return String(value).trim();
}

function formatTime(iso) {
  try {
    const date = new Date(iso);
    if (Number.isNaN(date.getTime())) return iso;
    return date.toLocaleString("zh-CN", { hour12: false });
  } catch (_err) {
    return iso;
  }
}

function normalizeUrl(raw) {
  const candidate =
    toSafeString(raw.url || raw.site_url || raw.web_url || raw.link || raw.homepage) ||
    toSafeString(raw.domain ? `https://${raw.domain}` : "");
  if (!candidate) {
    const domains = Array.isArray(raw.domains) ? raw.domains : [];
    const firstDomain = toSafeString(domains[0]);
    if (firstDomain) {
      return `https://${firstDomain}`;
    }
    return "";
  }

  if (/^https?:\/\//i.test(candidate)) {
    return candidate;
  }
  if (candidate.includes(".") && !candidate.includes(" ")) {
    return `https://${candidate}`;
  }
  return candidate;
}

function renderUrlCell(url) {
  const text = toSafeString(url);
  if (!text) {
    return "<span>-</span>";
  }
  const safeText = escapeHtml(text);
  const href = /^https?:\/\//i.test(text) ? safeText : `https://${safeText}`;
  return `<a href="${href}" target="_blank" rel="noreferrer noopener">${safeText}</a>`;
}

function escapeHtml(text) {
  return toSafeString(text)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}
