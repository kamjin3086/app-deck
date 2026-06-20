const state = {
  catalog: null,
  prefs: null,
  query: "",
  draggedId: null
};

const deck = document.querySelector("#deck");
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
  document.querySelector("#rescanBtn").addEventListener("click", async () => {
    state.catalog = await postJSON("/api/rescan", {});
    showToast("已重新扫描。");
    render();
  });
  document.querySelector("#addAppBtn").addEventListener("click", () => openAppDialog());
  document.querySelector("#addCatBtn").addEventListener("click", async () => {
    const name = prompt("新分组名称");
    if (!name) return;
    state.prefs.categories.push({ id: makeId("cat"), name: name.trim() });
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
  appForm.addEventListener("submit", async () => {
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
    await savePrefs("已保存应用。");
  });
  document.addEventListener("click", async event => {
    const addTo = event.target.dataset.addTo;
    const edit = event.target.dataset.edit;
    const hide = event.target.dataset.hide;
    const deleteCat = event.target.dataset.deleteCat;
    if (addTo) openAppDialog(null, addTo);
    if (edit) {
      const app = state.catalog.apps.find(item => item.id === edit);
      if (app) openAppDialog(app, app.categoryId);
    }
    if (hide && confirm("隐藏这个入口？")) {
      upsertOverride(hide, { hidden: true });
      await savePrefs("已隐藏入口。");
    }
    if (deleteCat) {
      const hasItems = state.catalog.apps.some(app => app.categoryId === deleteCat);
      if (hasItems) return showToast("这个分组还有应用，先移动或隐藏它们。", true);
      state.prefs.categories = state.prefs.categories.filter(cat => cat.id !== deleteCat);
      await savePrefs("已删除分组。");
    }
  });
}

function render() {
  if (!state.catalog) return;
  document.title = state.catalog.title || "AppDeck";
  const apps = filteredApps();
  const running = state.catalog.apps.filter(app => app.status === "running").length;
  document.querySelector("#summaryTitle").textContent = state.catalog.title || "AppDeck";
  document.querySelector("#summaryText").textContent = `${state.catalog.appsRoot} 下的本机入口，已合并 Docker、systemd 和你的手动偏好。`;
  document.querySelector("#runningCount").textContent = running;
  document.querySelector("#totalCount").textContent = state.catalog.apps.length;
  document.querySelector("#issueCount").textContent = state.catalog.issues?.length || 0;
  renderIssues();
  deck.innerHTML = "";
  for (const cat of state.catalog.categories) {
    const section = document.createElement("section");
    section.className = "category";
    section.dataset.categoryId = cat.id;
    section.innerHTML = `
      <div class="category-head">
        <input class="category-name" value="${escapeHTML(cat.name)}" aria-label="分组名称">
        <div class="category-actions">
          <button title="添加应用" data-add-to="${cat.id}">+</button>
          <button class="danger" title="删除空分组" data-delete-cat="${cat.id}">x</button>
        </div>
      </div>
      <div class="category-items"></div>
    `;
    const list = section.querySelector(".category-items");
    for (const app of apps.filter(item => item.categoryId === cat.id)) {
      list.appendChild(renderApp(app));
    }
    section.querySelector(".category-name").addEventListener("change", async event => {
      const prefCat = state.prefs.categories.find(item => item.id === cat.id);
      if (prefCat) {
        prefCat.name = event.target.value.trim() || prefCat.name;
        await savePrefs("已保存分组。");
      }
    });
    attachDrop(section);
    deck.appendChild(section);
  }
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
  box.innerHTML = issues.map(issue => `<div>${escapeHTML(issue.source)}: ${escapeHTML(issue.message)}</div>`).join("");
}

function filteredApps() {
  const q = state.query;
  if (!q) return state.catalog.apps;
  return state.catalog.apps.filter(app => [
    app.name, app.url, app.note, app.path, app.source, app.status,
    ...(app.ports || [])
  ].join(" ").toLowerCase().includes(q));
}

function renderApp(app) {
  const card = document.createElement("article");
  card.className = "app-card";
  card.draggable = true;
  card.dataset.id = app.id;
  card.dataset.status = app.status;
  const portLine = [
    app.source,
    app.status,
    ...(app.ports || [])
  ].filter(Boolean).join(" / ");
  card.innerHTML = `
    <div class="card-top">
      <div class="app-name">${escapeHTML(app.name)}</div>
      <div class="status-pill">${escapeHTML(app.status)}</div>
    </div>
    ${portLine ? `<div class="meta">${escapeHTML(portLine)}</div>` : ""}
    ${app.note ? `<div class="note">${escapeHTML(app.note)}</div>` : ""}
    ${app.url ? `<div class="meta">${escapeHTML(app.url)}</div>` : ""}
    ${app.path ? `<div class="path">${escapeHTML(app.path)}</div>` : ""}
    <div class="card-actions">
      ${app.url ? `<a href="${escapeAttr(app.url)}" target="_blank" rel="noopener">打开</a>` : ""}
      <button data-edit="${escapeAttr(app.id)}">编辑</button>
      <button class="danger" data-hide="${escapeAttr(app.id)}">隐藏</button>
    </div>
  `;
  card.addEventListener("dragstart", () => {
    state.draggedId = app.id;
    card.classList.add("dragging");
  });
  card.addEventListener("dragend", () => {
    state.draggedId = null;
    card.classList.remove("dragging");
    document.querySelectorAll(".drag-over").forEach(node => node.classList.remove("drag-over"));
  });
  return card;
}

function attachDrop(section) {
  section.addEventListener("dragover", event => {
    if (!state.draggedId) return;
    event.preventDefault();
    section.classList.add("drag-over");
  });
  section.addEventListener("dragleave", () => section.classList.remove("drag-over"));
  section.addEventListener("drop", async event => {
    event.preventDefault();
    const categoryId = section.dataset.categoryId;
    const targetCard = event.target.closest(".app-card");
    const idsInCategory = state.catalog.apps
      .filter(app => app.categoryId === categoryId && app.id !== state.draggedId)
      .map(app => app.id);
    const targetIndex = targetCard ? idsInCategory.indexOf(targetCard.dataset.id) : -1;
    const insertAt = targetIndex >= 0 ? targetIndex : idsInCategory.length;
    idsInCategory.splice(insertAt, 0, state.draggedId);
    idsInCategory.forEach((id, order) => upsertOverride(id, { categoryId, order }));
    await savePrefs("已移动入口。");
  });
}

function openAppDialog(app = null, categoryId = null) {
  document.querySelector("#appDialogTitle").textContent = app ? "编辑应用" : "添加应用";
  document.querySelector("#editId").value = app?.id || "";
  document.querySelector("#appName").value = app?.name || "";
  document.querySelector("#appUrl").value = app?.url || "";
  document.querySelector("#appNote").value = app?.note || "";
  document.querySelector("#appPath").value = app?.path || "";
  const select = document.querySelector("#appCategory");
  select.innerHTML = state.prefs.categories.map(cat => `<option value="${escapeAttr(cat.id)}">${escapeHTML(cat.name)}</option>`).join("");
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
