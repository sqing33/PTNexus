const CONFIG_KEY = "ptnexus_cookie_sync_config";
const TOKEN_KEY = "ptnexus_cookie_sync_token";
const SYNC_REPORT_KEY = "ptnexus_cookie_sync_last_report";
const TOKEN_SAFE_WINDOW_MS = 60 * 1000;
const API_BASE_URL_FALLBACK_MAP = new Map();
let hasShownApiFallbackNotice = false;

const els = {
  apiBaseUrl: null,
  username: null,
  password: null,
  rememberPassword: null,
  syncBtn: null,
  status: null,
};

document.addEventListener("DOMContentLoaded", async () => {
  bindElements();
  bindEvents();
  await loadConfigToForm();
});

function bindElements() {
  els.apiBaseUrl = document.getElementById("apiBaseUrl");
  els.username = document.getElementById("username");
  els.password = document.getElementById("password");
  els.rememberPassword = document.getElementById("rememberPassword");
  els.syncBtn = document.getElementById("syncBtn");
  els.status = document.getElementById("status");
}

function bindEvents() {
  els.syncBtn.addEventListener("click", onSync);
}

async function onSync() {
  setBusy(true);
  setStatus("正在测试登录...", "info");

  try {
    const config = readConfigFromForm({ requirePassword: false });
    const originalApiBaseUrl = config.apiBaseUrl;
    await persistConfig(config);
    refreshConfigApiBaseUrl(config);

    await ensureApiOriginPermission(config.apiBaseUrl);
    const targets = await callWithAuthRetry(config, (token) => fetchCookieSyncTargets(config.apiBaseUrl, token));
    refreshConfigApiBaseUrl(config);
    const switchedApiBaseUrl = config.apiBaseUrl !== originalApiBaseUrl;
    if (switchedApiBaseUrl) {
      setStatus(`已自动切换为 HTTP：${config.apiBaseUrl}，登录测试通过，准备同步...`, "warning");
    } else {
      setStatus("登录测试通过，准备同步...", "info");
    }
    if (!targets.length) {
      const report = {
        generated_at: new Date().toISOString(),
        success: true,
        message: "后端未返回任何站点，同步已结束。",
        api_base_url: config.apiBaseUrl,
        username: config.username,
        total_targets: 0,
        updated_count: 0,
        skipped_count: 0,
        failed_count: 0,
        results: [],
      };
      await storageSet({ [SYNC_REPORT_KEY]: report });
      setStatus("后端未返回任何站点。", "warning");
      await openResultsPage();
      return;
    }

    await ensureOriginsPermission(config.apiBaseUrl, targets);

    const targetInfoByKey = buildTargetInfoByKey(targets);

    // 并行收集 Cookie（分批处理，每批 10 个）
    const BATCH_SIZE = 10;
    const items = [];
    let completedCount = 0;

    for (let i = 0; i < targets.length; i += BATCH_SIZE) {
      const batch = targets.slice(i, i + BATCH_SIZE);

      // 并行处理当前批次
      const batchResults = await Promise.all(
        batch.map(async (target) => {
          const cookieRequired = target.cookie_required !== false;
          const cookie = cookieRequired ? await collectCookieStringForSite(target) : "";
          return {
            site: toSafeString(target.site),
            nickname: toSafeString(target.nickname),
            url: targetToUrl(target),
            domains: Array.isArray(target.domains) ? target.domains : [],
            cookie,
          };
        })
      );

      items.push(...batchResults);
      completedCount += batch.length;
      setStatus(`正在收集 Cookie（${completedCount}/${targets.length}）...`, "info");
    }

    setStatus("正在提交同步请求...", "info");
    const syncResponse = await callWithAuthRetry(config, (token) =>
      postCookieSyncBatch(config.apiBaseUrl, token, {
        items,
        skip_empty: true,
      }),
    );

    const results = enrichResultsWithTargetInfo(syncResponse?.results, targetInfoByKey);

    // 重新计算真正的失败数（排除"未命中站点"和"未更新"等成功状态）
    const realFailedCount = countRealFailures(results);
    const successCount = targets.length - realFailedCount;

    const report = {
      generated_at: new Date().toISOString(),
      success: realFailedCount === 0,
      message: syncResponse.message || "同步完成。",
      api_base_url: config.apiBaseUrl,
      username: config.username,
      total_targets: targets.length,
      submitted_items: items.length,
      updated_count: syncResponse.updated_count || 0,
      skipped_count: syncResponse.skipped_count || 0,
      failed_count: realFailedCount,
      results,
    };

    if (realFailedCount > 0) {
      setStatus(`同步完成：成功 ${successCount} 个，失败 ${realFailedCount} 个（已打开结果页）。`, "warning");
    } else {
      setStatus(`同步完成：全部成功 ${successCount} 个站点（已打开结果页）。`, "success");
    }
    await storageSet({ [SYNC_REPORT_KEY]: report });
    await openResultsPage();
  } catch (error) {
    setStatus(error.message || "同步失败。", "error");
  } finally {
    setBusy(false);
  }
}

function readConfigFromForm({ requirePassword }) {
  const apiBaseUrl = normalizeApiBaseUrl(els.apiBaseUrl.value);
  const username = toSafeString(els.username.value);
  const password = String(els.password.value || "");
  const rememberPassword = Boolean(els.rememberPassword.checked);

  if (!apiBaseUrl) {
    throw new Error("请输入 API 地址。");
  }
  if (!username) {
    throw new Error("请输入用户名。");
  }
  if (requirePassword && !password) {
    throw new Error("请输入密码。");
  }

  return { apiBaseUrl, username, password, rememberPassword };
}

async function persistConfig(config) {
  const payload = {
    apiBaseUrl: config.apiBaseUrl,
    username: config.username,
    rememberPassword: config.rememberPassword,
    password: config.rememberPassword ? config.password : "",
  };
  await storageSet({ [CONFIG_KEY]: payload });
}

async function loadConfigToForm() {
  try {
    const data = await storageGet(CONFIG_KEY);
    const config = data[CONFIG_KEY] || {};
    els.apiBaseUrl.value = toSafeString(config.apiBaseUrl);
    els.username.value = toSafeString(config.username);
    els.rememberPassword.checked = Boolean(config.rememberPassword);
    els.password.value = config.rememberPassword ? toSafeString(config.password) : "";
  } catch (error) {
    setStatus(`加载配置失败：${error.message || error}`, "error");
  }
}

async function callWithAuthRetry(config, callback) {
  let token = await ensureToken(config, { forceLogin: false });
  try {
    return await callback(token);
  } catch (error) {
    if (error && error.status === 401) {
      token = await ensureToken(config, { forceLogin: true });
      return callback(token);
    }
    throw error;
  }
}

async function ensureToken(config, { forceLogin }) {
  if (!forceLogin) {
    const cached = await storageGet(TOKEN_KEY);
    const tokenRecord = cached[TOKEN_KEY];
    if (isTokenRecordUsable(tokenRecord, config)) {
      return tokenRecord.token;
    }
  }

  if (!config.password) {
    throw new Error("请输入密码以重新登录。");
  }
  return loginAndStoreToken(config);
}

async function loginAndStoreToken(config) {
  const apiBaseUrl = refreshConfigApiBaseUrl(config);
  await ensureApiOriginPermission(apiBaseUrl);

  const data = await requestJSON(apiBaseUrl, "/api/auth/login", {
    method: "POST",
    payload: {
      username: config.username,
      password: config.password,
    },
  });

  if (!data.success || !data.token) {
    throw new Error(data.message || "登录失败。");
  }

  const expiresAt = decodeTokenExpiryMs(data.token);
  const tokenRecord = {
    token: data.token,
    expiresAt,
    apiBaseUrl: refreshConfigApiBaseUrl(config),
    username: config.username,
  };
  await storageSet({ [TOKEN_KEY]: tokenRecord });
  return data.token;
}

async function fetchCookieSyncTargets(apiBaseUrl, token) {
  const data = await requestJSON(apiBaseUrl, "/api/sites/cookie_sync_targets", {
    method: "GET",
    token,
  });
  if (!Array.isArray(data.sites)) {
    return [];
  }
  return data.sites;
}

async function postCookieSyncBatch(apiBaseUrl, token, payload) {
  return requestJSON(apiBaseUrl, "/api/sites/cookie_sync_batch", {
    method: "POST",
    token,
    payload,
  });
}

async function ensureApiOriginPermission(apiBaseUrl) {
  const patterns = buildApiOriginPatterns(apiBaseUrl);
  if (!patterns.length) {
    return;
  }
  const hasPermission = await permissionsContains(patterns);
  if (hasPermission) {
    return;
  }
  const granted = await permissionsRequest(patterns);
  if (!granted) {
    throw new Error(`未授权后端地址访问权限：${patterns.join(" / ")}`);
  }
}

async function ensureOriginsPermission(apiBaseUrl, targets) {
  const origins = new Set();
  for (const pattern of buildApiOriginPatterns(apiBaseUrl)) {
    origins.add(pattern);
  }

  for (const target of targets) {
    const domains = Array.isArray(target.domains) ? target.domains : [];
    for (const rawDomain of domains) {
      const domain = toSafeString(rawDomain).toLowerCase();
      if (!isUsableDomain(domain)) {
        continue;
      }
      origins.add(`https://${domain}/*`);
      origins.add(`http://${domain}/*`);

      // 同时申请根域名的权限（如 qingwapt.com），以便获取 .qingwapt.com 域下的 cookie
      const rootDomain = extractRootDomain(domain);
      if (rootDomain && rootDomain !== domain) {
        origins.add(`https://${rootDomain}/*`);
        origins.add(`http://${rootDomain}/*`);
      }
    }
  }

  const originList = Array.from(origins);
  if (!originList.length) {
    return;
  }

  const hasAll = await permissionsContains(originList);
  if (hasAll) {
    return;
  }

  const granted = await permissionsRequest(originList);
  if (!granted) {
    throw new Error("所需站点访问权限被拒绝，请授权后重试。");
  }
}

async function collectCookieStringForSite(target) {
  const domains = Array.isArray(target.domains) ? target.domains : [];
  if (!domains.length) {
    return "";
  }

  const cookieMap = new Map();
  for (const rawDomain of domains) {
    const domain = toSafeString(rawDomain).toLowerCase();
    if (!isUsableDomain(domain)) {
      continue;
    }

    const cookies = await getCookiesByDomain(domain);
    for (const cookie of cookies) {
      if (!cookie || !cookie.name) {
        continue;
      }
      cookieMap.set(cookie.name, cookie.value || "");
    }
  }

  const parts = [];
  for (const [name, value] of cookieMap.entries()) {
    parts.push(`${name}=${value}`);
  }
  return parts.join("; ");
}

async function getCookiesByDomain(domain) {
  const allCookies = [];
  const seen = new Set();

  // 收集 cookies 并去重
  const addCookies = (cookies, source) => {
    console.log(`[Cookie Debug] ${source}: found ${cookies?.length || 0} cookies`);
    for (const cookie of cookies || []) {
      console.log(`[Cookie Debug] ${source}: ${cookie.name}=${cookie.value?.substring(0, 20)}... domain=${cookie.domain} path=${cookie.path}`);
      const key = `${cookie.name}|${cookie.domain}|${cookie.path || "/"}`;
      if (!seen.has(key)) {
        seen.add(key);
        allCookies.push(cookie);
      }
    }
  };

  console.log(`[Cookie Debug] Getting cookies for domain: ${domain}`);

  const rootDomain = extractRootDomain(domain);

  // 构建所有查询条件，一次性并行执行
  const queries = [];
  queries.push(cookiesGetAll({ url: `https://${domain}/` }));

  if (rootDomain && rootDomain !== domain) {
    queries.push(cookiesGetAll({ url: `https://${rootDomain}/` }));
    queries.push(cookiesGetAll({ domain: `.${rootDomain}` }));
    queries.push(cookiesGetAll({ domain: rootDomain }));
  }

  queries.push(cookiesGetAll({ domain }));
  queries.push(cookiesGetAll({ domain: `.${domain}` }));

  // 并行执行所有查询
  const results = await Promise.all(queries);
  for (const cookies of results) {
    addCookies(cookies);
  }

  return allCookies;
}

// 从域名中提取根域名（如 www.qingwapt.com -> qingwapt.com）
function extractRootDomain(domain) {
  const parts = domain.split(".");
  if (parts.length <= 2) {
    return domain;
  }
  // 返回最后两部分作为根域名
  return parts.slice(-2).join(".");
}

async function requestJSON(apiBaseUrl, path, options) {
  const primaryApiBaseUrl = getEffectiveApiBaseUrl(apiBaseUrl);
  try {
    return await requestJSONOnce(primaryApiBaseUrl, path, options);
  } catch (primaryError) {
    if (!shouldFallbackToHTTP(primaryApiBaseUrl, primaryError)) {
      throw primaryError;
    }

    const fallbackApiBaseUrl = toHttpFallbackOrigin(primaryApiBaseUrl);
    if (!fallbackApiBaseUrl || fallbackApiBaseUrl === primaryApiBaseUrl) {
      throw createProtocolMismatchError(primaryError, null, primaryApiBaseUrl);
    }

    try {
      const data = await requestJSONOnce(fallbackApiBaseUrl, path, options);
      await persistApiBaseUrlFallback(primaryApiBaseUrl, fallbackApiBaseUrl);
      showApiFallbackNotice(fallbackApiBaseUrl);
      return data;
    } catch (fallbackError) {
      throw createProtocolMismatchError(primaryError, fallbackError, primaryApiBaseUrl);
    }
  }
}

async function requestJSONOnce(apiBaseUrl, path, options) {
  const method = options.method || "GET";
  const headers = {};
  if (options.token) {
    headers.Authorization = `Bearer ${options.token}`;
  }
  if (options.payload !== undefined) {
    headers["Content-Type"] = "application/json";
  }

  const response = await fetch(`${apiBaseUrl}${path}`, {
    method,
    headers,
    body: options.payload !== undefined ? JSON.stringify(options.payload) : undefined,
  });

  const text = await response.text();
  let data = {};
  if (text) {
    try {
      data = JSON.parse(text);
    } catch (_error) {
      data = { message: text };
    }
  }

  if (!response.ok) {
    const message = data.message || data.error || `HTTP ${response.status}`;
    const error = new Error(message);
    error.status = response.status;
    error.data = data;
    throw error;
  }

  return data;
}

function normalizeApiBaseUrl(raw) {
  const trimmed = toSafeString(raw).replace(/\/$/, "");
  if (!trimmed) {
    return "";
  }
  const value = /^https?:\/\//i.test(trimmed) ? trimmed : `${inferDefaultScheme(trimmed)}://${trimmed}`;
  try {
    const parsed = new URL(value);
    return parsed.origin;
  } catch (_error) {
    throw new Error("API 地址格式不正确。");
  }
}

function decodeTokenExpiryMs(token) {
  try {
    const parts = token.split(".");
    if (parts.length < 2) {
      return Date.now() + 6 * 24 * 60 * 60 * 1000;
    }
    const payload = parts[1].replace(/-/g, "+").replace(/_/g, "/");
    const padded = payload + "=".repeat((4 - (payload.length % 4)) % 4);
    const decoded = atob(padded);
    const parsed = JSON.parse(decoded);
    if (typeof parsed.exp === "number" && parsed.exp > 0) {
      return parsed.exp * 1000;
    }
    return Date.now() + 6 * 24 * 60 * 60 * 1000;
  } catch (_error) {
    return Date.now() + 6 * 24 * 60 * 60 * 1000;
  }
}

function isTokenRecordUsable(tokenRecord, config) {
  if (!tokenRecord || typeof tokenRecord !== "object") {
    return false;
  }
  if (toSafeString(tokenRecord.token) === "") {
    return false;
  }
  const tokenApiBaseUrl = toSafeString(tokenRecord.apiBaseUrl);
  const configApiBaseUrl = toSafeString(config.apiBaseUrl);
  const expectedApiBaseUrl = getEffectiveApiBaseUrl(configApiBaseUrl);
  if (tokenApiBaseUrl !== expectedApiBaseUrl && tokenApiBaseUrl !== configApiBaseUrl) {
    return false;
  }
  if (toSafeString(tokenRecord.username) !== config.username) {
    return false;
  }
  const expiresAt = Number(tokenRecord.expiresAt || 0);
  return Number.isFinite(expiresAt) && expiresAt > Date.now() + TOKEN_SAFE_WINDOW_MS;
}

function isUsableDomain(domain) {
  if (!domain || /\s/.test(domain)) {
    return false;
  }
  if (domain.includes("/") || domain.includes("@")) {
    return false;
  }
  if (domain === "localhost") {
    return true;
  }
  if (/^\d{1,3}(\.\d{1,3}){3}$/.test(domain)) {
    return true;
  }
  return domain.includes(".");
}

function refreshConfigApiBaseUrl(config) {
  if (!config || typeof config !== "object") {
    return "";
  }
  const effectiveApiBaseUrl = getEffectiveApiBaseUrl(config.apiBaseUrl);
  if (effectiveApiBaseUrl && effectiveApiBaseUrl !== config.apiBaseUrl) {
    config.apiBaseUrl = effectiveApiBaseUrl;
  }
  return toSafeString(config.apiBaseUrl);
}

function getEffectiveApiBaseUrl(apiBaseUrl) {
  const normalized = toSafeString(apiBaseUrl);
  if (!normalized) {
    return "";
  }
  return API_BASE_URL_FALLBACK_MAP.get(normalized) || normalized;
}

function buildApiOriginPatterns(apiBaseUrl) {
  try {
    const parsed = new URL(getEffectiveApiBaseUrl(apiBaseUrl) || apiBaseUrl);
    return [`https://${parsed.host}/*`, `http://${parsed.host}/*`];
  } catch (_error) {
    return [];
  }
}

function inferDefaultScheme(raw) {
  const value = toSafeString(raw);
  if (!value) {
    return "https";
  }
  try {
    const parsed = new URL(/^https?:\/\//i.test(value) ? value : `http://${value}`);
    return isPrivateOrLocalHost(parsed.hostname) ? "http" : "https";
  } catch (_error) {
    return "https";
  }
}

function isPrivateOrLocalHost(hostname) {
  const host = toSafeString(hostname).toLowerCase();
  if (!host) {
    return false;
  }
  if (host === "localhost" || host === "::1" || host.endsWith(".local")) {
    return true;
  }

  const ipv4 = parseIPv4(host);
  if (ipv4) {
    const [first, second] = ipv4;
    if (first === 10 || first === 127) return true;
    if (first === 192 && second === 168) return true;
    if (first === 172 && second >= 16 && second <= 31) return true;
    if (first === 169 && second === 254) return true;
    if (first === 100 && second >= 64 && second <= 127) return true;
  }

  const unwrapped = host.replace(/^\[/, "").replace(/\]$/, "");
  if (unwrapped === "::1" || unwrapped.startsWith("fe80:")) {
    return true;
  }
  if (unwrapped.startsWith("fc") || unwrapped.startsWith("fd")) {
    return true;
  }
  return false;
}

function parseIPv4(hostname) {
  if (!/^\d{1,3}(\.\d{1,3}){3}$/.test(hostname)) {
    return null;
  }
  const numbers = hostname.split(".").map((part) => Number(part));
  if (numbers.some((num) => !Number.isInteger(num) || num < 0 || num > 255)) {
    return null;
  }
  return numbers;
}

function shouldFallbackToHTTP(apiBaseUrl, error) {
  if (!/^https:\/\//i.test(toSafeString(apiBaseUrl))) {
    return false;
  }
  return !hasHttpStatus(error);
}

function hasHttpStatus(error) {
  if (!error || typeof error !== "object") {
    return false;
  }
  const status = Number(error.status);
  return Number.isFinite(status) && status > 0;
}

function toHttpFallbackOrigin(apiBaseUrl) {
  try {
    const parsed = new URL(apiBaseUrl);
    if (parsed.protocol !== "https:") {
      return "";
    }
    return `http://${parsed.host}`;
  } catch (_error) {
    return "";
  }
}

function createProtocolMismatchError(primaryError, fallbackError, apiBaseUrl) {
  const messages = [];
  const primaryMessage = toSafeString(primaryError?.message);
  if (primaryMessage) {
    messages.push(primaryMessage);
  }
  const fallbackMessage = toSafeString(fallbackError?.message);
  if (fallbackMessage) {
    messages.push(`HTTP 回退失败：${fallbackMessage}`);
  }
  const httpHint = buildHTTPHint(apiBaseUrl);
  messages.push(`检测到 HTTPS 连接异常，请确认后端协议；updater 默认使用 HTTP（如 ${httpHint}）。`);

  const error = new Error(messages.join("；"));
  if (hasHttpStatus(fallbackError)) {
    error.status = fallbackError.status;
    error.data = fallbackError.data;
  } else if (hasHttpStatus(primaryError)) {
    error.status = primaryError.status;
    error.data = primaryError.data;
  }
  return error;
}

function buildHTTPHint(apiBaseUrl) {
  try {
    const parsed = new URL(apiBaseUrl);
    return `http://${parsed.host}`;
  } catch (_error) {
    return "http://<你的服务器IP>:5274";
  }
}

async function persistApiBaseUrlFallback(fromApiBaseUrl, toApiBaseUrl) {
  const from = toSafeString(fromApiBaseUrl);
  const to = toSafeString(toApiBaseUrl);
  if (!from || !to || from === to) {
    return;
  }

  API_BASE_URL_FALLBACK_MAP.set(from, to);
  API_BASE_URL_FALLBACK_MAP.set(to, to);

  try {
    const data = await storageGet([CONFIG_KEY, TOKEN_KEY]);
    const updates = {};

    const config = data[CONFIG_KEY];
    if (config && typeof config === "object" && toSafeString(config.apiBaseUrl) === from) {
      updates[CONFIG_KEY] = {
        ...config,
        apiBaseUrl: to,
      };
    }

    const tokenRecord = data[TOKEN_KEY];
    if (tokenRecord && typeof tokenRecord === "object" && toSafeString(tokenRecord.apiBaseUrl) === from) {
      updates[TOKEN_KEY] = {
        ...tokenRecord,
        apiBaseUrl: to,
      };
    }

    if (Object.keys(updates).length > 0) {
      await storageSet(updates);
    }
  } catch (_error) {
    // 存储失败不影响本次同步流程
  }
}

function showApiFallbackNotice(fallbackApiBaseUrl) {
  if (hasShownApiFallbackNotice || !toSafeString(fallbackApiBaseUrl)) {
    return;
  }
  hasShownApiFallbackNotice = true;
  setStatus(`检测到 HTTPS 不可用，已自动切换为 HTTP：${fallbackApiBaseUrl}`, "warning");
}

function setBusy(busy) {
  els.syncBtn.disabled = busy;
}

function setStatus(message, level) {
  els.status.className = `status ${level}`;
  els.status.textContent = message;
}

function toSafeString(value) {
  if (value === null || value === undefined) {
    return "";
  }
  return String(value).trim();
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

function storageSet(value) {
  return new Promise((resolve, reject) => {
    chrome.storage.local.set(value, () => {
      if (chrome.runtime.lastError) {
        reject(new Error(chrome.runtime.lastError.message));
        return;
      }
      resolve();
    });
  });
}

function permissionsContains(origins) {
  return new Promise((resolve, reject) => {
    chrome.permissions.contains({ origins }, (granted) => {
      if (chrome.runtime.lastError) {
        reject(new Error(chrome.runtime.lastError.message));
        return;
      }
      resolve(Boolean(granted));
    });
  });
}

function permissionsRequest(origins) {
  return new Promise((resolve, reject) => {
    chrome.permissions.request({ origins }, (granted) => {
      if (chrome.runtime.lastError) {
        reject(new Error(chrome.runtime.lastError.message));
        return;
      }
      resolve(Boolean(granted));
    });
  });
}

function cookiesGetAll(details) {
  return new Promise((resolve, reject) => {
    chrome.cookies.getAll(details, (cookies) => {
      if (chrome.runtime.lastError) {
        reject(new Error(chrome.runtime.lastError.message));
        return;
      }
      resolve(Array.isArray(cookies) ? cookies : []);
    });
  });
}

async function openResultsPage() {
  const url = chrome.runtime.getURL("results.html");
  await new Promise((resolve, reject) => {
    chrome.tabs.create({ url }, () => {
      if (chrome.runtime.lastError) {
        reject(new Error(chrome.runtime.lastError.message));
        return;
      }
      resolve();
    });
  });
}

function makeTargetKey(site, nickname) {
  return `${toSafeString(site).toLowerCase()}::${toSafeString(nickname).toLowerCase()}`;
}

function buildTargetInfoByKey(targets) {
  const map = new Map();
  for (const target of targets) {
    const site = toSafeString(target?.site);
    const nickname = toSafeString(target?.nickname);
    const info = {
      url: targetToUrl(target),
      domains: Array.isArray(target?.domains) ? target.domains : [],
    };
    if (site || nickname) {
      map.set(makeTargetKey(site, nickname), info);
      if (site) {
        map.set(makeTargetKey(site, ""), info);
      }
    }
  }
  return map;
}

function targetToUrl(target) {
  const url = toSafeString(target?.url || target?.site_url || target?.web_url || target?.homepage);
  if (url) {
    if (/^https?:\/\//i.test(url)) return url;
    return `https://${url}`;
  }
  const domains = Array.isArray(target?.domains) ? target.domains : [];
  const domain = toSafeString(domains[0]);
  return domain ? `https://${domain}` : "";
}

function enrichResultsWithTargetInfo(results, targetInfoByKey) {
  const list = Array.isArray(results) ? results : [];
  return list.map((item) => {
    if (!item || typeof item !== "object") {
      return item;
    }
    const site = toSafeString(item.site || item.name || item.host || item.domain);
    const nickname = toSafeString(item.nickname || item.title);
    const info =
      targetInfoByKey.get(makeTargetKey(site, nickname)) ||
      targetInfoByKey.get(makeTargetKey(site, "")) ||
      targetInfoByKey.get(makeTargetKey("", nickname)) ||
      null;
    if (!info) {
      return item;
    }
    const patched = { ...item };
    if (!toSafeString(patched.url)) {
      patched.url = info.url;
    }
    if (!Array.isArray(patched.domains) && Array.isArray(info.domains) && info.domains.length) {
      patched.domains = info.domains;
    }
    return patched;
  });
}

// 计算真正的失败数量（排除"未命中站点"和"未更新"等成功状态）
function countRealFailures(results) {
  const list = Array.isArray(results) ? results : [];
  let failedCount = 0;
  for (const item of list) {
    if (!item || typeof item !== "object") {
      continue;
    }
    const msg = toSafeString(item.message || item.detail || item.reason);
    // "未更新"、"未命中站点" 等都算成功
    if (msg.includes("未更新") || msg.includes("无需更新") || msg.includes("不需要更新") || msg.includes("未命中站点")) {
      continue;
    }
    // 明确标记为成功的
    if (item.success === true) continue;
    // updated=false 也算成功（无需更新）
    if (item.updated === false || item.is_updated === false) continue;

    const status = toSafeString(item.status || item.state || item.result).toLowerCase();
    // 成功状态
    if (["ok", "success", "succeeded", "passed", "updated", "done", "unchanged", "nochange", "no_change", "not_updated"].includes(status)) {
      continue;
    }
    // 跳过状态也算成功
    if (["skip", "skipped", "ignored", "empty"].includes(status)) continue;
    if (status.includes("skip")) continue;
    if (status.includes("not updated") || status.includes("not_updated") || status.includes("unchanged") || status.includes("no change")) continue;
    if (status.includes("success") || status.includes("ok")) continue;

    // 如果明确标记为失败
    if (item.success === false) {
      failedCount++;
      continue;
    }
    // 失败状态
    if (["fail", "failed", "error", "denied", "rejected"].includes(status)) {
      failedCount++;
      continue;
    }
    if (status.includes("fail") || status.includes("error")) {
      failedCount++;
      continue;
    }

    // 未知状态，保守地算作失败
    if (status) {
      failedCount++;
    }
  }
  return failedCount;
}
