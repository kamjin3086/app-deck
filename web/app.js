const state = {
  catalog: null,
  prefs: null,
  query: "",
  activeCategory: "all",
  recentIds: loadRecentIds(),
  draggedId: null
};

const deck = document.querySelector("#deck");
const categoryNav = document.querySelector("#categoryNav");
const recentColumn = document.querySelector("#recentColumn");
const search = document.querySelector("#search");
const toast = document.querySelector("#toast");
const appDialog = document.querySelector("#appDialog");
const appForm = document.querySelector("#appForm");

init().catch(err => showToast(err.message || String(err), true));

async function init() {
  await loadAll();
  bindEvents();
}

async function loadAll() {
  const [catalog, prefs] = await Promise.all([
    getJSON("/api/apps"),
    getJSON("/api/export")
  ]);
  state.catalog = catalog;
  state.prefs = prefs;
  render();
}

function bindEvents() {
  search.addEventListener("input", () => {
    state.query = search.value.trim().toLowerCase();
    render();
  });

  document.querySelector("#showAllBtn").addEventListener("click", () => {
    state.activeCategory = "all";
    render();
  });

  document.querySelector("#rescanBtn").addEventListener("click", async () => {
    state.catalog = await postJSON("/api/rescan", {});
    showToast("已重新扫描。");
    render();
  });

  document.querySelector("#addAppBtn").addEventListener("click", () => {
    const categoryId = ["all", "recent"].includes(state.activeCategory) ? null : state.activeCategory;
    openAppDialog(null, categoryId);
  });

  document.querySelector("#addCatBtn").addEventListener("click", async () => {
    const name = prompt("新分组名称");
    if (!name) return;
    const id = makeId("cat");
    state.prefs.categories.push({ id, name: name.trim() });
    state.activeCategory = id;
    await savePrefs("已添加分组。");
  });

  document.querySelector("#exportBtn").addEventListener("click", () => {
    window.location.href = "/api/export";
  });

  document.querySelector("#importInput").addEventListener("change", async event => {
    const file = event.target.files[0];
    if (!file) return;
    const prefs = JSON.parse(await file.text());
    state.catalog = await postJSON("/api/import", prefs);
    state.prefs = await getJSON("/api/export");
    event.target.value = "";
    showToast("已导入配置。");
    render();
  });

  document.querySelector("#cancelAppBtn").addEventListener("click", () => appDialog.close());

  appForm.addEventListener("submit", async event => {
    event.preventDefault();
    const id = document.querySelector("#editId").value || makeId("manual");
    const categoryId = document.querySelector("#appCategory").value;
    const app = {
      id,
      name: document.querySelector("#appName").value.trim(),
      url: document.querySelector("#appUrl").value.trim(),
      note: document.querySelector("#appNote").value.trim(),
      path: document.querySelector("#appPath").value.trim(),
      categoryId,
      source: "manual",
      status: "unknown"
    };
    if (id.startsWith("manual:")) {
      const index = state.prefs.manualApps.findIndex(item => item.id === id);
      if (index >= 0) state.prefs.manualApps.splice(index, 1, app);
      else state.prefs.manualApps.push(app);
    }
    upsertOverride(id, {
      name: app.name,
      url: app.url,
      note: app.note,
      path: app.path,
      categoryId
    });
    appDialog.close();
    state.activeCategory = categoryId;
    await savePrefs("已保存应用。");
  });

  document.addEventListener("click", async event => {
    const open = event.target.closest("[data-open]");
    const edit = event.target.dataset.edit;
    const hide = event.target.dataset.hide;
    if (open) {
      recordOpen(open.dataset.open);
      render();
    }
    if (edit) {
      event.preventDefault();
      const app = state.catalog.apps.find(item => item.id === edit);
      if (app) openAppDialog(app, app.categoryId);
    }
    if (hide && confirm("隐藏这个入口？")) {
      event.preventDefault();
      upsertOverride(hide, { hidden: true });
      await savePrefs("已隐藏入口。");
    }
  });
}

function render() {
  if (!state.catalog) return;
  document.title = state.catalog.title || "AppDeck";

  if (!["all", "recent"].includes(state.activeCategory) && !state.catalog.categories.some(cat => cat.id === state.activeCategory)) {
    state.activeCategory = "all";
  }

  const apps = filteredApps();
  const running = state.catalog.apps.filter(app => app.status === "running").length;
  const activeName = state.activeCategory === "all"
    ? "全部入口"
    : state.activeCategory === "recent"
      ? "最近使用"
    : state.catalog.categories.find(cat => cat.id === state.activeCategory)?.name || "当前分组";

  document.querySelector("#pageTitle").textContent = state.catalog.title || "AppDeck";
  document.querySelector("#resultMeta").textContent =
    `${state.catalog.apps.length} 个入口 · ${running} 个运行中 · 当前 ${activeName}`;

  renderIssues();
  renderCategoryNav();
  renderRecentColumn();
  renderDeck(apps);
}

function renderCategoryNav() {
  const allButton = document.querySelector("#showAllBtn");
  allButton.textContent = `全部 ${state.catalog.apps.length}`;
  allButton.classList.toggle("active", state.activeCategory === "all");

  const counts = new Map();
  for (const app of state.catalog.apps) {
    counts.set(app.categoryId, (counts.get(app.categoryId) || 0) + 1);
  }

  categoryNav.innerHTML = "";
  categoryNav.appendChild(renderRecentLink());
  for (const cat of state.catalog.categories) {
    const button = document.createElement("button");
    button.className = "cat-link";
    button.classList.toggle("active", cat.id === state.activeCategory);
    button.dataset.categoryId = cat.id;
    button.innerHTML = `
      <span class="cat-name">${escapeHTML(cat.name)}</span>
      <span class="cat-count">${counts.get(cat.id) || 0}</span>
    `;
    button.addEventListener("click", () => {
      state.activeCategory = cat.id;
      render();
    });
    button.addEventListener("dblclick", async () => {
      const name = prompt("重命名分组", cat.name);
      if (!name) return;
      const prefCat = state.prefs.categories.find(item => item.id === cat.id);
      if (prefCat) {
        prefCat.name = name.trim() || prefCat.name;
        await savePrefs("已保存分组。");
      }
    });
    attachCategoryDrop(button);
    categoryNav.appendChild(button);
  }
}

function renderRecentLink() {
  const availableIds = new Set(state.catalog.apps.map(app => app.id));
  const count = state.recentIds.filter(id => availableIds.has(id)).length;
  const button = document.createElement("button");
  button.className = "cat-link recent-link";
  button.classList.toggle("active", state.activeCategory === "recent");
  button.innerHTML = `
    <span class="cat-name">最近使用</span>
    <span class="cat-count">${count}</span>
  `;
  button.addEventListener("click", () => {
    state.activeCategory = "recent";
    render();
  });
  return button;
}

function renderDeck(apps) {
  deck.innerHTML = "";
  if (!apps.length) {
    const empty = document.createElement("div");
    empty.className = "empty-state";
    empty.textContent = emptyMessage();
    deck.appendChild(empty);
    attachDeckDrop(deck);
    return;
  }
  for (const app of apps) {
    deck.appendChild(renderApp(app));
  }
  attachDeckDrop(deck);
}

function renderRecentColumn() {
  const appsById = new Map(state.catalog.apps.map(app => [app.id, app]));
  const recentApps = state.recentIds.map(id => appsById.get(id)).filter(Boolean).slice(0, 5);
  recentColumn.innerHTML = `
    <div class="recent-head">
      <h3>最近使用</h3>
      <span>${recentApps.length}/5</span>
    </div>
    <div class="recent-list">
      ${recentApps.length ? recentApps.map(renderRecentItem).join("") : `
        <div class="recent-empty">打开几个应用后，这里会变成你的快捷 Dock。</div>
      `}
    </div>
  `;
}

function renderRecentItem(app) {
  const primaryLine = app.url || app.path || "未设置入口";
  const attrs = app.url
    ? `href="${escapeAttr(app.url)}" target="_blank" rel="noopener" data-open="${escapeAttr(app.id)}"`
    : `href="#" data-edit="${escapeAttr(app.id)}"`;
  return `
    <a class="recent-item" ${attrs}>
      ${appIcon(app, "small")}
      <span>
        <strong>${escapeHTML(app.name)}</strong>
        <small>${escapeHTML(shortURL(primaryLine))}</small>
      </span>
    </a>
  `;
}

function renderIssues() {
  const issues = state.catalog.issues || [];
  const box = document.querySelector("#issues");
  if (!issues.length) {
    box.hidden = true;
    box.innerHTML = "";
    return;
  }
  box.hidden = false;
  box.innerHTML = issues.slice(0, 2)
    .map(issue => `<div>${escapeHTML(issue.source)}: ${escapeHTML(issue.message)}</div>`)
    .join("");
}

function filteredApps() {
  const q = state.query;
  const recentRank = new Map(state.recentIds.map((id, index) => [id, index]));
  return state.catalog.apps.filter(app => {
    const inCategory = state.activeCategory === "all"
      || app.categoryId === state.activeCategory
      || (state.activeCategory === "recent" && recentRank.has(app.id));
    if (!inCategory) return false;
    if (!q) return true;
    return [
      app.name,
      app.url,
      app.note,
      app.path,
      app.source,
      app.status,
      ...(app.ports || [])
    ].join(" ").toLowerCase().includes(q);
  }).sort((a, b) => {
    if (state.activeCategory !== "recent") return 0;
    return recentRank.get(a.id) - recentRank.get(b.id);
  });
}

function renderApp(app) {
  const card = document.createElement("article");
  card.className = "app-card";
  card.draggable = true;
  card.dataset.id = app.id;
  card.dataset.status = app.status;
  const primaryLine = app.url || app.path || app.source || "未设置入口";
  const detailItems = [
    ["状态", app.status],
    ["来源", app.source],
    ["端口", (app.ports || []).join(", ")],
    ["路径", app.path],
    ["ID", app.id]
  ].filter(([, value]) => value);

  card.innerHTML = `
    <div class="card-top">
      ${appIcon(app)}
      <span class="status-dot" title="${escapeAttr(app.status)}"></span>
    </div>
    <div class="app-main">
      <div class="app-name">${escapeHTML(app.name)}</div>
      <div class="app-url" title="${escapeAttr(primaryLine)}">${escapeHTML(shortURL(primaryLine))}</div>
      ${app.note ? `<div class="app-note">${escapeHTML(app.note)}</div>` : ""}
    </div>
    <div class="card-actions">
      ${app.url ? `<a class="open-link" href="${escapeAttr(app.url)}" target="_blank" rel="noopener" data-open="${escapeAttr(app.id)}">打开</a>` : `<button class="open-link" data-edit="${escapeAttr(app.id)}">补入口</button>`}
      <button class="text-action" data-edit="${escapeAttr(app.id)}">编辑</button>
      <button class="text-action danger" data-hide="${escapeAttr(app.id)}">隐藏</button>
    </div>
    <details class="detail-row">
      <summary>详情</summary>
      <dl>
        ${detailItems.map(([label, value]) => `
          <div><dt>${escapeHTML(label)}</dt><dd>${escapeHTML(value)}</dd></div>
        `).join("")}
      </dl>
    </details>
  `;
  card.addEventListener("dragstart", () => {
    state.draggedId = app.id;
    card.classList.add("dragging");
  });
  card.addEventListener("dragend", () => {
    state.draggedId = null;
    card.classList.remove("dragging");
    document.querySelectorAll(".drop-target").forEach(node => node.classList.remove("drop-target"));
  });
  return card;
}

function attachCategoryDrop(button) {
  button.addEventListener("dragover", event => {
    if (!state.draggedId) return;
    event.preventDefault();
    button.classList.add("drop-target");
  });
  button.addEventListener("dragleave", () => button.classList.remove("drop-target"));
  button.addEventListener("drop", async event => {
    event.preventDefault();
    const categoryId = button.dataset.categoryId;
    moveDraggedApp(categoryId);
  });
}

function attachDeckDrop(target) {
  target.ondragover = event => {
    if (!state.draggedId) return;
    if (state.activeCategory === "recent") return;
    const canDropHere = state.activeCategory !== "all" || event.target.closest(".app-card");
    if (!canDropHere) return;
    event.preventDefault();
  };
  target.ondrop = async event => {
    if (!state.draggedId) return;
    if (state.activeCategory === "recent") return;
    event.preventDefault();
    const targetCard = event.target.closest(".app-card");
    const targetApp = targetCard
      ? state.catalog.apps.find(app => app.id === targetCard.dataset.id)
      : null;
    const categoryId = state.activeCategory === "all"
      ? targetApp?.categoryId
      : state.activeCategory;
    if (!categoryId) return;
    moveDraggedApp(categoryId, targetCard?.dataset.id);
  };
}

function emptyMessage() {
  if (state.query) return "没有匹配的入口。";
  if (state.activeCategory === "recent") return "打开几个应用后，这里会自动保留最近使用的 5 个。";
  return "这个分组还没有入口，可以添加应用或把卡片拖到这里。";
}

async function moveDraggedApp(categoryId, beforeId = "") {
  const idsInCategory = state.catalog.apps
    .filter(app => app.categoryId === categoryId && app.id !== state.draggedId)
    .map(app => app.id);
  const targetIndex = beforeId ? idsInCategory.indexOf(beforeId) : -1;
  const insertAt = targetIndex >= 0 ? targetIndex : idsInCategory.length;
  idsInCategory.splice(insertAt, 0, state.draggedId);
  idsInCategory.forEach((id, order) => upsertOverride(id, { categoryId, order }));
  state.activeCategory = categoryId;
  await savePrefs("已移动入口。");
}

function openAppDialog(app = null, categoryId = null) {
  document.querySelector("#appDialogTitle").textContent = app ? "编辑应用" : "添加应用";
  document.querySelector("#editId").value = app?.id || "";
  document.querySelector("#appName").value = app?.name || "";
  document.querySelector("#appUrl").value = app?.url || "";
  document.querySelector("#appNote").value = app?.note || "";
  document.querySelector("#appPath").value = app?.path || "";
  const select = document.querySelector("#appCategory");
  select.innerHTML = state.prefs.categories
    .map(cat => `<option value="${escapeAttr(cat.id)}">${escapeHTML(cat.name)}</option>`)
    .join("");
  select.value = categoryId || app?.categoryId || state.prefs.categories[0]?.id || "pending";
  appDialog.showModal();
}

function upsertOverride(id, patch) {
  const current = state.prefs.overrides[id] || {};
  const next = { ...current };
  for (const [key, value] of Object.entries(patch)) {
    next[key] = value;
  }
  state.prefs.overrides[id] = next;
}

async function savePrefs(message) {
  state.catalog = await postJSON("/api/preferences", state.prefs);
  state.prefs = await getJSON("/api/export");
  showToast(message);
  render();
}

async function getJSON(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(await errorText(res));
  return res.json();
}

async function postJSON(url, body) {
  const res = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body)
  });
  if (!res.ok) throw new Error(await errorText(res));
  return res.json();
}

async function errorText(res) {
  try {
    const data = await res.json();
    return data.error || res.statusText;
  } catch {
    return res.statusText;
  }
}

function shortURL(value) {
  try {
    const url = new URL(value);
    return url.host + url.pathname.replace(/\/$/, "");
  } catch {
    return value;
  }
}

function appIcon(app, size = "large") {
  const iconURL = app.url ? `/api/icon?url=${encodeURIComponent(app.url)}` : "";
  const initialsText = initials(app.name);
  const hue = hashHue(app.id || app.name);
  return `
    <span class="app-icon ${size === "small" ? "app-icon-small" : ""}" style="--icon-hue:${hue}">
      ${iconURL ? `<img src="${escapeAttr(iconURL)}" alt="" loading="lazy" onerror="this.remove()">` : ""}
      <span>${escapeHTML(initialsText)}</span>
    </span>
  `;
}

function initials(name) {
  const clean = String(name || "App").replace(/[^\p{L}\p{N}\s]/gu, " ").trim();
  const parts = clean.split(/\s+/).filter(Boolean);
  if (!parts.length) return "A";
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[1][0]).toUpperCase();
}

function hashHue(value) {
  let hash = 0;
  for (const ch of String(value || "appdeck")) {
    hash = (hash * 31 + ch.charCodeAt(0)) % 360;
  }
  return hash;
}

function loadRecentIds() {
  try {
    const data = JSON.parse(localStorage.getItem("appdeck.recentOpenIds") || "[]");
    return Array.isArray(data) ? data.slice(0, 5) : [];
  } catch {
    return [];
  }
}

function recordOpen(id) {
  if (!id) return;
  state.recentIds = [id, ...state.recentIds.filter(item => item !== id)].slice(0, 5);
  localStorage.setItem("appdeck.recentOpenIds", JSON.stringify(state.recentIds));
}

function makeId(prefix) {
  return `${prefix}:${crypto.randomUUID ? crypto.randomUUID() : Math.random().toString(36).slice(2)}`;
}

function showToast(message, error = false) {
  toast.textContent = message;
  toast.style.borderColor = error ? "var(--ember)" : "var(--line)";
  toast.classList.add("show");
  clearTimeout(showToast.timer);
  showToast.timer = setTimeout(() => toast.classList.remove("show"), 2400);
}

function escapeHTML(value) {
  return String(value ?? "").replace(/[&<>"']/g, ch => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#039;" }[ch]));
}

function escapeAttr(value) {
  return escapeHTML(value);
}
