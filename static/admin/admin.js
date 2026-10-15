/* Admin — themed Docs-like editor. No EasyMDE. Rich text -> markdown via Turndown. */
const state = {
  section: "blogs",
  items: [],
  counts: { blogs: 0, projects: 0, pieces: 0 },
  selectedId: null,
  mode: "new",
  dirty: false,
  lastSavedSnapshot: "",
  tags: [],
  listQuery: "",
  slugTouched: false,
  previewRequestId: 0,
  previewTimer: null,
  statusOverride: "",
  draggedEntryId: null,
  reorderInFlight: false,
  blogDates: { datePublished: "", lastUpdated: "" },
  docMode: "write", // write | preview
  turndown: null,
  saveInFlight: false,
};

const dom = {};
const sectionMeta = {
  blogs: { label: "Blogs", singular: "Blog Post", newLabel: "New Blog Post", endpoint: "/api/admin/blogs" },
  projects: { label: "Projects", singular: "Project", newLabel: "New Project", endpoint: "/api/admin/projects" },
  pieces: { label: "Pieces", singular: "Piece", newLabel: "New Piece", endpoint: "/api/admin/pieces" },
};

document.addEventListener("DOMContentLoaded", () => {
  cacheDom();
  bindGlobalEvents();
  boot();
});

function cacheDom() {
  dom.loginView = document.getElementById("login-view");
  dom.appView = document.getElementById("app-view");
  dom.loginForm = document.getElementById("login-form");
  dom.passwordInput = document.getElementById("password");
  dom.loginError = document.getElementById("login-error");
  dom.navToggle = document.getElementById("nav-toggle");
  dom.topLinks = document.getElementById("top-links");
  dom.tabs = Array.from(document.querySelectorAll(".tab"));
  dom.createEntryButton = document.getElementById("create-entry");
  dom.logoutButton = document.getElementById("logout-button");
  dom.saveStatus = document.getElementById("save-status");
  dom.publicLink = document.getElementById("public-link");
  dom.listHeading = document.getElementById("list-heading");
  dom.listKicker = document.getElementById("list-kicker");
  dom.listTotal = document.getElementById("list-total");
  dom.listSearch = document.getElementById("list-search");
  dom.listNote = document.getElementById("list-note");
  dom.entryList = document.getElementById("entry-list");
  dom.listEmpty = document.getElementById("list-empty");
  dom.editorRoot = document.getElementById("editor-root");
  dom.toastRoot = document.getElementById("toast-root");
  dom.modalRoot = document.getElementById("modal-root");
  dom.modalTitle = document.getElementById("modal-title");
  dom.modalDesc = document.getElementById("modal-desc");
  dom.modalFields = document.getElementById("modal-fields");
  dom.modalCancel = document.getElementById("modal-cancel");
  dom.modalConfirm = document.getElementById("modal-confirm");
  dom.slashMenu = document.getElementById("slash-menu");
  dom.countBlogs = document.getElementById("count-blogs");
  dom.countProjects = document.getElementById("count-projects");
  dom.countPieces = document.getElementById("count-pieces");
}

function bindGlobalEvents() {
  dom.loginForm.addEventListener("submit", handleLogin);
  dom.logoutButton.addEventListener("click", handleLogout);
  dom.createEntryButton.addEventListener("click", () => startNewEntry());
  dom.listSearch.addEventListener("input", () => {
    state.listQuery = dom.listSearch.value.trim().toLowerCase();
    updateListNote();
    renderList();
  });
  dom.tabs.forEach((b) => b.addEventListener("click", async () => {
    if (b.dataset.section === state.section) return;
    if (!confirmDiscardIfNeeded()) return;
    await setSection(b.dataset.section);
  }));
  if (dom.navToggle) dom.navToggle.addEventListener("click", () => {
    const links = document.querySelector(".top-bar .links");
    if (!links) return;
    const open = links.classList.toggle("open");
    dom.navToggle.setAttribute("aria-expanded", open ? "true" : "false");
  });
  document.addEventListener("keydown", (e) => {
    const mod = e.metaKey || e.ctrlKey;
    if (mod && e.key.toLowerCase() === "s") {
      if (dom.appView.classList.contains("hidden")) return;
      e.preventDefault();
      saveCurrentEntry();
    }
    if (e.key === "Escape") {
      hideSlashMenu();
      if (!dom.modalRoot.classList.contains("hidden")) closeModal(null);
    }
  });
  document.addEventListener("selectionchange", updateToolbarStates);
  document.addEventListener("click", (e) => {
    if (!dom.slashMenu.classList.contains("hidden") && !dom.slashMenu.contains(e.target)) hideSlashMenu();
  });
}

/* ---------- boot / auth ---------- */
async function boot() {
  const authed = await checkAuthentication();
  const onLoginRoute = window.location.pathname === "/admin/login";
  if (!authed) {
    showLogin();
    if (!onLoginRoute) window.location.replace("/admin/login");
    return;
  }
  if (onLoginRoute) { window.location.replace("/admin"); return; }
  showApp();
  initTurndown();
  await refreshCounts();
  await setSection(state.section);
}
async function checkAuthentication() {
  try { await fetchJSON("/api/admin/me"); return true; } catch { return false; }
}
function showLogin() { dom.loginView.classList.remove("hidden"); dom.appView.classList.add("hidden"); }
function showApp() { dom.loginView.classList.add("hidden"); dom.appView.classList.remove("hidden"); }
async function handleLogin(e) {
  e.preventDefault();
  hideLoginError();
  try {
    await fetchJSON("/api/admin/login", { method: "POST", body: JSON.stringify({ password: dom.passwordInput.value }) });
    window.location.replace("/admin");
  } catch (err) { showLoginError(err.message || "Login failed."); }
}
async function handleLogout() {
  try { await fetchJSON("/api/admin/logout", { method: "POST" }); }
  finally { window.location.replace("/admin/login"); }
}

/* ---------- sections / list ---------- */
async function setSection(name) {
  state.section = name;
  state.items = [];
  state.selectedId = null;
  state.mode = "new";
  state.tags = [];
  state.listQuery = "";
  state.slugTouched = false;
  state.draggedEntryId = null;
  state.reorderInFlight = false;
  state.statusOverride = "";
  state.docMode = "write";
  state.blogDates = { datePublished: "", lastUpdated: "" };
  dom.listSearch.value = "";
  hideSlashMenu();
  updateSectionChrome();
  await loadCurrentSection();
}
function updateSectionChrome() {
  const meta = sectionMeta[state.section];
  dom.tabs.forEach((b) => b.classList.toggle("active", b.dataset.section === state.section));
  dom.listKicker.textContent = meta.label;
  dom.listHeading.textContent = `All ${meta.label.toLowerCase()}`;
  const btnLabel = state.section === "blogs" ? "+ New post" : state.section === "projects" ? "+ New project" : "+ New piece";
  dom.createEntryButton.textContent = btnLabel;
  updateListNote();
  setDirty(false);
  updatePublicLink(null);
  renderCounts();
}
function updateListNote() {
  if (state.section !== "projects") { dom.listNote.textContent = ""; dom.listNote.classList.add("hidden"); return; }
  dom.listNote.textContent = state.listQuery
    ? "Search is active — clear it to drag projects into public order."
    : "Drag to control the exact order on the public projects page. ↑↓ buttons work on touch.";
  dom.listNote.classList.remove("hidden");
}
async function refreshCounts() {
  try {
    const [b, p, c] = await Promise.allSettled([
      fetchJSON("/api/admin/blogs"), fetchJSON("/api/admin/projects"), fetchJSON("/api/admin/pieces"),
    ]);
    if (b.status === "fulfilled" && Array.isArray(b.value)) state.counts.blogs = b.value.length;
    if (p.status === "fulfilled" && Array.isArray(p.value)) state.counts.projects = p.value.length;
    if (c.status === "fulfilled" && Array.isArray(c.value)) state.counts.pieces = c.value.length;
    renderCounts();
  } catch { /* ignore */ }
}
function renderCounts() {
  if (dom.countBlogs) dom.countBlogs.textContent = String(state.counts.blogs || 0);
  if (dom.countProjects) dom.countProjects.textContent = String(state.counts.projects || 0);
  if (dom.countPieces) dom.countPieces.textContent = String(state.counts.pieces || 0);
}
async function loadCurrentSection(preferId) {
  const meta = sectionMeta[state.section];
  dom.entryList.innerHTML = "";
  dom.listEmpty.textContent = `Loading ${meta.label.toLowerCase()}…`;
  dom.listEmpty.classList.remove("hidden");
  try {
    const items = await fetchJSON(meta.endpoint);
    state.items = Array.isArray(items) ? items : [];
    if (state.section === "blogs") state.counts.blogs = state.items.length;
    if (state.section === "projects") state.counts.projects = state.items.length;
    if (state.section === "pieces") state.counts.pieces = state.items.length;
    renderCounts();
    renderList();
    if (preferId && state.items.find((x) => getItemId(x) === preferId)) { await selectEntry(preferId); return; }
    if (state.items.length > 0) { await selectEntry(getItemId(state.items[0])); return; }
    startNewEntry({ skipConfirm: true });
  } catch (err) {
    dom.entryList.innerHTML = "";
    dom.listEmpty.textContent = err.message || `Failed to load ${meta.label.toLowerCase()}.`;
    dom.listEmpty.classList.remove("hidden");
    startNewEntry({ skipConfirm: true });
  }
}
function filteredItems() {
  if (!state.listQuery) return state.items;
  return state.items.filter((item) => {
    const hay = [item.title, item.slug, item.description, item.category, item.date_start, item.date_end, item.video_url,
      Array.isArray(item.tags) ? item.tags.join(" ") : ""].filter(Boolean).join(" ").toLowerCase();
    return hay.includes(state.listQuery);
  });
}
function renderList() {
  const items = filteredItems();
  dom.entryList.innerHTML = "";
  dom.listTotal.textContent = `${items.length} item${items.length === 1 ? "" : "s"}`;
  if (items.length === 0) {
    dom.listEmpty.textContent = state.listQuery ? "No matching entries." : "Nothing here yet — create your first one.";
    dom.listEmpty.classList.remove("hidden");
    return;
  }
  dom.listEmpty.classList.add("hidden");
  items.forEach((item, idx) => {
    const card = createEntryCard(item);
    if (canReorderProjects()) { dom.entryList.appendChild(createProjectRow(item, card, idx, items.length)); return; }
    dom.entryList.appendChild(card);
  });
}
function createEntryCard(item) {
  const card = document.createElement("button");
  const id = getItemId(item);
  card.type = "button";
  card.className = "entry-card" + (id === state.selectedId ? " active" : "");
  card.innerHTML = renderListCard(item);
  card.addEventListener("click", async () => {
    if (id === state.selectedId) return;
    if (!confirmDiscardIfNeeded()) return;
    await selectEntry(id);
  });
  return card;
}
function renderListCard(item) {
  if (state.section === "blogs") {
    const st = normalizeBlogStatus(item.status || "published");
    return `<h4>${escapeHtml(item.title || "Untitled post")}</h4>
      <div class="entry-meta"><span class="badge badge-${st}">${escapeHtml(st)}</span>
      <span>${escapeHtml(item.category || "uncategorized")}</span><span>${escapeHtml(item.date_published || "no date")}</span></div>
      <p class="entry-snippet">${escapeHtml(item.description || "No description yet.")}</p>`;
  }
  if (state.section === "projects") {
    const dates = [item.date_start, item.date_end].filter(Boolean).join(" – ");
    const tags = Array.isArray(item.tags) && item.tags.length ? item.tags.join(", ") : "no tags";
    return `<h4>${escapeHtml(item.title || "Untitled project")}</h4>
      <div class="entry-meta"><span>${escapeHtml(dates || "no dates")}</span></div>
      <p class="entry-snippet">${escapeHtml(item.description || tags)}</p>`;
  }
  return `<h4>${escapeHtml(item.title || "Untitled piece")}</h4>
    <div class="entry-meta"><span>${escapeHtml(truncate(item.video_url || "no video url", 42))}</span></div>
    <p class="entry-snippet">${escapeHtml(item.description || "No description yet.")}</p>`;
}
function canReorderProjects() { return state.section === "projects" && !state.listQuery; }
function createProjectRow(item, card, idx, total) {
  const id = getItemId(item);
  const row = document.createElement("div");
  row.className = "entry-row" + (id === state.selectedId ? " active" : "");
  row.dataset.id = id;
  row.draggable = !state.reorderInFlight;
  const handle = document.createElement("div");
  handle.className = "entry-drag-handle";
  handle.title = "Drag to reorder";
  handle.textContent = "⠿";
  handle.setAttribute("aria-hidden", "true");
  row.appendChild(handle);
  const wrap = document.createElement("div");
  wrap.style.minWidth = "0";
  wrap.appendChild(card);
  const btns = document.createElement("div");
  btns.className = "row-move-btns";
  const up = document.createElement("button");
  up.type = "button"; up.className = "mini-btn"; up.textContent = "↑"; up.disabled = idx === 0;
  up.addEventListener("click", (e) => { e.stopPropagation(); moveProjectByIndex(idx, idx - 1); });
  const down = document.createElement("button");
  down.type = "button"; down.className = "mini-btn"; down.textContent = "↓"; down.disabled = idx === total - 1;
  down.addEventListener("click", (e) => { e.stopPropagation(); moveProjectByIndex(idx, idx + 1); });
  btns.appendChild(up); btns.appendChild(down);
  wrap.appendChild(btns);
  row.appendChild(wrap);
  bindProjectRowDrag(row, id);
  return row;
}
function bindProjectRowDrag(row, id) {
  row.addEventListener("dragstart", (e) => {
    if (state.reorderInFlight) { e.preventDefault(); return; }
    state.draggedEntryId = id;
    row.classList.add("is-dragging");
    if (e.dataTransfer) { e.dataTransfer.effectAllowed = "move"; e.dataTransfer.setData("text/plain", id); }
  });
  row.addEventListener("dragover", (e) => {
    if (!state.draggedEntryId || state.draggedEntryId === id) return;
    e.preventDefault();
    const after = pointerPastMid(e, row);
    row.classList.toggle("drop-before", !after);
    row.classList.toggle("drop-after", after);
  });
  row.addEventListener("dragleave", (e) => {
    if (e.relatedTarget instanceof Node && row.contains(e.relatedTarget)) return;
    row.classList.remove("drop-before", "drop-after");
  });
  row.addEventListener("drop", async (e) => {
    e.preventDefault();
    const dragged = state.draggedEntryId || (e.dataTransfer && e.dataTransfer.getData("text/plain"));
    const after = pointerPastMid(e, row);
    row.classList.remove("drop-before", "drop-after");
    if (!dragged || dragged === id) return;
    await moveProjectEntry(dragged, id, after);
  });
  row.addEventListener("dragend", () => { state.draggedEntryId = null; clearDragState(); });
}
function pointerPastMid(e, row) { const r = row.getBoundingClientRect(); return e.clientY > r.top + r.height / 2; }
function clearDragState() { dom.entryList.querySelectorAll(".entry-row").forEach((r) => r.classList.remove("is-dragging", "drop-before", "drop-after")); }
async function moveProjectByIndex(from, to) {
  if (state.reorderInFlight || to < 0 || to >= state.items.length) return;
  const ids = state.items.map(getItemId);
  const [m] = ids.splice(from, 1);
  ids.splice(to, 0, m);
  await persistProjectOrder(ids);
}
async function moveProjectEntry(sourceId, targetId, after) {
  if (state.reorderInFlight) return;
  const prev = [...state.items];
  const si = prev.findIndex((x) => getItemId(x) === sourceId);
  const ti = prev.findIndex((x) => getItemId(x) === targetId);
  if (si === -1 || ti === -1) return;
  const re = [...prev];
  const [mv] = re.splice(si, 1);
  let ins = re.findIndex((x) => getItemId(x) === targetId);
  if (after) ins += 1;
  re.splice(ins, 0, mv);
  const nextIds = re.map(getItemId);
  if (nextIds.join("|") === prev.map(getItemId).join("|")) return;
  await persistProjectOrder(nextIds, prev, re);
}
async function persistProjectOrder(nextIds, prev, optimistic) {
  const previousItems = prev || [...state.items];
  if (optimistic) { state.items = optimistic; }
  else {
    const map = new Map(previousItems.map((x) => [getItemId(x), x]));
    state.items = nextIds.map((id) => map.get(id)).filter(Boolean);
  }
  state.reorderInFlight = true;
  setStatusOverride("Saving order…");
  renderList();
  try {
    const res = await fetchJSON("/api/admin/projects/reorder", { method: "POST", body: JSON.stringify({ ids: nextIds }) });
    state.items = Array.isArray(res && res.projects) ? res.projects : state.items;
    toast("Order saved");
  } catch (err) {
    state.items = previousItems;
    toast(err.message || "Reorder failed", true);
  } finally {
    state.reorderInFlight = false;
    state.draggedEntryId = null;
    setStatusOverride("");
    renderList();
  }
}

/* ---------- select / new ---------- */
async function selectEntry(id) {
  hideSlashMenu();
  state.selectedId = id;
  state.mode = "edit";
  renderList();
  const meta = sectionMeta[state.section];
  dom.editorRoot.innerHTML = `<div class="editor-empty"><p class="eyebrow">Loading</p><h3>Opening ${escapeHtml(meta.singular.toLowerCase())}…</h3></div>`;
  try {
    const item = await fetchJSON(`${meta.endpoint}/${id}`);
    renderEditor(item);
  } catch (err) {
    dom.editorRoot.innerHTML = `<div class="editor-empty"><p class="eyebrow">Error</p><h3>Couldn’t load entry</h3><p class="muted">${escapeHtml(err.message || "")}</p></div>`;
  }
}
function startNewEntry(opts = {}) {
  if (!opts.skipConfirm && !confirmDiscardIfNeeded()) return;
  hideSlashMenu();
  state.selectedId = null;
  state.mode = "new";
  renderList();
  renderEditor(defaultItemForSection(state.section));
}
function renderEditor(item) {
  state.tags = Array.isArray(item.tags) ? [...item.tags] : [];
  state.slugTouched = state.mode === "edit";
  state.docMode = "write";
  state.blogDates = {
    datePublished: item && typeof item.date_published === "string" ? item.date_published : "",
    lastUpdated: item && typeof item.last_updated === "string" ? item.last_updated : "",
  };
  if (state.section === "blogs") { renderBlogEditor(item); updatePublicLink(item); return; }
  if (state.section === "projects") { dom.editorRoot.innerHTML = renderProjectEditor(item); bindProjectEditor(item); updatePublicLink(null); return; }
  dom.editorRoot.innerHTML = renderPieceEditor(item); bindPieceEditor(item); updatePublicLink(null);
}

/* ================= BLOG — Google Docs style ================= */
function renderBlogEditor(item) {
  const st = normalizeBlogStatus(item.status);
  const hasPub = Boolean(item.date_published);
  const hasUpd = Boolean(item.last_updated);
  dom.editorRoot.innerHTML = `
  <div class="docs-shell">
    <div class="docs-titlebar">
      <div class="docs-title-row">
        <div style="flex:1;min-width:0">
          <p class="eyebrow">${state.mode === "edit" ? "Editing · " + escapeHtml(st) : "New draft"}</p>
          <input id="doc-title" class="doc-title-input" placeholder="Untitled post" value="${escapeAttr(item.title || "")}" autocomplete="off">
          <p class="muted mono" style="font-size:.85rem">Type <b>/</b> for insert menu · <b>#</b>, <b>-</b>, <b>&gt;</b>, <b>\`\`\`</b> work like markdown · ⌘S saves</p>
        </div>
        <div class="docs-actions">
          <div class="mode-toggle" role="tablist">
            <button type="button" id="mode-write" class="active">Write</button>
            <button type="button" id="mode-preview">Preview</button>
          </div>
          <button type="button" id="save-doc" class="primary-button">Save</button>
        </div>
      </div>
    </div>

    <details class="docs-settings" id="doc-settings">
      <summary><span>Post settings — slug, category, cover, SEO</span><span class="mono" id="settings-state">＋</span></summary>
      <div class="settings-grid">
        <div class="field"><label for="f-slug">Slug</label>
          <div class="slug-row"><input id="f-slug" value="${escapeAttr(item.slug || "")}" placeholder="my-first-post"><button type="button" id="regen-slug" class="mini-btn" title="Regenerate from title">↻</button></div>
          <span class="field-error hidden" id="err-slug"></span>
        </div>
        <div class="field"><label for="f-category">Category</label>
          <input id="f-category" value="${escapeAttr(item.category || "")}" placeholder="Engineering">
          <span class="field-error hidden" id="err-category"></span>
        </div>
        <div class="field"><label for="f-status">Status</label>
          <select id="f-status">
            <option value="draft"${st === "draft" ? " selected" : ""}>Draft — hidden from site</option>
            <option value="published"${st === "published" ? " selected" : ""}>Published — live on site</option>
          </select>
        </div>
        <div class="field"><label for="f-thumbnail">Cover image URL</label>
          <input id="f-thumbnail" value="${escapeAttr(item.thumbnail || "")}" placeholder="https://… /img/post.avif">
          <span class="field-error hidden" id="err-thumbnail"></span>
        </div>
        <div class="field field-span-2"><label for="f-description">Description (SEO + cards)</label>
          <textarea id="f-description" placeholder="One or two sentences that sell the post…">${escapeHtml(item.description || "")}</textarea>
          <span class="field-error hidden" id="err-description"></span>
        </div>
        <div class="field field-span-2">
          <span class="field-label">Cover preview + timestamps</span>
          <div class="thumb-preview">
            <img id="thumb-img" src="${escapeAttr(item.thumbnail || "")}" alt="" style="${item.thumbnail ? "" : "display:none"}">
            <div class="auto-meta" style="flex:1">
              <div><span>Uploaded</span><strong>${escapeHtml(hasPub ? item.date_published : "auto on first save")}</strong></div>
              <div><span>Updated</span><strong>${escapeHtml(hasUpd ? item.last_updated : "auto on every save")}</strong></div>
            </div>
          </div>
          <span class="field-error hidden" id="err-title"></span>
          <span class="field-error hidden" id="err-markdown"></span>
        </div>
      </div>
    </details>

    <div id="docs-write-pane">
      <div class="docs-toolbar-wrap">
        <div class="docs-toolbar" id="docs-toolbar" aria-label="Formatting toolbar">
          <div class="tb-group">
            <button type="button" class="tb-btn" data-cmd="undo" title="Undo (⌘Z)">↩</button>
            <button type="button" class="tb-btn" data-cmd="redo" title="Redo (⇧⌘Z)">↪</button>
          </div>
          <div class="tb-group">
            <select id="block-style" class="tb-select" title="Paragraph style">
              <option value="p">Normal</option>
              <option value="h1">Title</option>
              <option value="h2">Heading 1</option>
              <option value="h3">Heading 2</option>
              <option value="blockquote">Quote</option>
              <option value="pre">Code block</option>
            </select>
          </div>
          <div class="tb-group">
            <button type="button" class="tb-btn" data-cmd="bold" title="Bold (⌘B)"><b>B</b></button>
            <button type="button" class="tb-btn" data-cmd="italic" title="Italic (⌘I)"><i>I</i></button>
            <button type="button" class="tb-btn" data-cmd="underline" title="Underline (⌘U)"><u>U</u></button>
            <button type="button" class="tb-btn" data-cmd="strike" title="Strikethrough"><s>S</s></button>
            <button type="button" class="tb-btn" data-cmd="inlinecode" title="Inline code">{ }</button>
          </div>
          <div class="tb-group">
            <button type="button" class="tb-btn" data-cmd="ul" title="Bulleted list">•≡</button>
            <button type="button" class="tb-btn" data-cmd="ol" title="Numbered list">1≡</button>
            <button type="button" class="tb-btn" data-cmd="quote" title="Quote">❝</button>
            <button type="button" class="tb-btn" data-cmd="codeblock" title="Code block">⌨</button>
            <button type="button" class="tb-btn" data-cmd="hr" title="Divider">―</button>
          </div>
          <div class="tb-group">
            <button type="button" class="tb-btn" data-cmd="link" title="Insert link">🔗</button>
            <button type="button" class="tb-btn" data-cmd="image" title="Insert image">🖼</button>
            <button type="button" class="tb-btn" data-cmd="table" title="Insert table">▦</button>
            <button type="button" class="tb-btn" data-cmd="clear" title="Clear formatting">✕</button>
          </div>
          <span class="tb-hint mono" id="doc-stats-top">0 words</span>
        </div>
      </div>
      <div class="docs-scroller">
        <div class="docs-page-wrap">
          <div id="docs-editor" class="docs-page" contenteditable="true" spellcheck="true" data-placeholder="Start writing… press / for blocks"></div>
        </div>
      </div>
      <div class="docs-statusbar mono">
        <span id="stat-words">0 words</span><span id="stat-chars">0 chars</span><span id="stat-read">0 min read</span><span id="stat-md">0 md chars</span><span id="autosave-note">saved</span>
      </div>
      <div class="docs-footer">
        <div class="docs-footer-left">
          <button type="button" id="save-doc-2" class="primary-button">Save post</button>
          ${state.mode === "edit" ? `<button type="button" id="delete-doc" class="danger-button">Delete</button>` : ""}
        </div>
        <div class="docs-footer-right"><span class="save-hint mono">Drafts stay hidden · ⌘S saves · autosaves locally</span></div>
      </div>
    </div>

    <div id="docs-preview-pane" class="docs-preview hidden">
      <div class="preview-card">
        <div class="preview-hero">
          <p class="eyebrow" id="pv-cat">preview</p>
          <h1 id="pv-title">Untitled post</h1>
          <p id="pv-desc">Add a description…</p>
          <p class="preview-meta" id="pv-meta"></p>
        </div>
        <div class="preview-thumb"><img id="pv-thumb" alt="" style="display:none"></div>
        <div class="preview-body"><div id="pv-body" class="markdown-rendered"><p class="muted">Nothing to preview yet.</p></div></div>
      </div>
      <div class="docs-footer">
        <div class="docs-footer-left">
          <button type="button" id="back-write" class="ghost-button">← Back to writing</button>
          <button type="button" id="save-doc-3" class="primary-button">Save post</button>
        </div>
        <span class="save-hint mono">Preview matches the public post page exactly.</span>
      </div>
    </div>
  </div>`;

  bindBlogEditor(item);
}

function bindBlogEditor(item) {
  const editor = document.getElementById("docs-editor");
  const titleInput = document.getElementById("doc-title");
  const slugInput = document.getElementById("f-slug");
  const catInput = document.getElementById("f-category");
  const statusSel = document.getElementById("f-status");
  const thumbInput = document.getElementById("f-thumbnail");
  const descInput = document.getElementById("f-description");
  const thumbImg = document.getElementById("thumb-img");

  // initial HTML from markdown
  const initialMd = item.markdown || "";
  editor.innerHTML = initialMd ? markdownToHtml(initialMd) : "<p><br></p>";
  ensureTrailingParagraph(editor);

  const savedSnapshot = snapshotForState({
    title: item.title || "", slug: item.slug || "", category: item.category || "",
    status: normalizeBlogStatus(item.status), thumbnail: item.thumbnail || "",
    description: item.description || "", markdown: item.markdown || "",
  });

  // restore autosave if newer/different
  const restored = restoreAutosave(blogAutosaveKey(item));
  if (restored && JSON.stringify(restored) !== JSON.stringify({ title: item.title, slug: item.slug, category: item.category, status: item.status, thumbnail: item.thumbnail, description: item.description, markdown: item.markdown })) {
    hydrateBlogForm(restored);
    toast("Restored local autosave");
  }

  const onChange = () => {
    persistBlogAutosave();
    updateDirtyState();
    schedulePreviewRefresh();
    updatePublicLink(currentFormData());
    updateDocStats();
    refreshPreviewHeader();
  };

  titleInput.addEventListener("input", () => {
    if (!state.slugTouched) slugInput.value = slugify(titleInput.value);
    clearFieldError("err-title");
    onChange();
  });
  slugInput.addEventListener("input", () => { state.slugTouched = true; clearFieldError("err-slug"); onChange(); });
  document.getElementById("regen-slug").addEventListener("click", () => {
    slugInput.value = slugify(titleInput.value);
    state.slugTouched = false;
    onChange();
  });
  [catInput, statusSel, thumbInput, descInput].forEach((el) => {
    el.addEventListener("input", onChange);
    el.addEventListener("change", onChange);
  });
  thumbInput.addEventListener("input", () => {
    const v = thumbInput.value.trim();
    if (v) { thumbImg.src = v; thumbImg.style.display = ""; }
    else thumbImg.style.display = "none";
  });
  const settings = document.getElementById("doc-settings");
  const settingsState = document.getElementById("settings-state");
  settings.addEventListener("toggle", () => { settingsState.textContent = settings.open ? "–" : "＋"; });

  editor.addEventListener("input", (e) => {
    handleMarkdownShortcuts(editor);
    handleSlashTrigger(editor);
    updateDocStats();
    onChange();
  });
  editor.addEventListener("keyup", updateToolbarStates);
  editor.addEventListener("mouseup", updateToolbarStates);
  editor.addEventListener("paste", (e) => handlePaste(e, editor, onChange));
  editor.addEventListener("keydown", (e) => handleEditorKeys(e, editor, onChange));
  editor.addEventListener("focus", () => { if (!editor.innerHTML.trim()) editor.innerHTML = "<p><br></p>"; });

  bindToolbar(editor, onChange);

  const styleSel = document.getElementById("block-style");
  styleSel.addEventListener("change", () => {
    editor.focus();
    applyBlockStyle(styleSel.value);
    onChange();
  });

  // mode toggle
  document.getElementById("mode-write").addEventListener("click", () => setDocMode("write"));
  document.getElementById("mode-preview").addEventListener("click", () => setDocMode("preview"));
  const backBtn = document.getElementById("back-write");
  if (backBtn) backBtn.addEventListener("click", () => setDocMode("write"));

  const save = () => saveCurrentEntry();
  ["save-doc", "save-doc-2", "save-doc-3"].forEach((id) => {
    const b = document.getElementById(id);
    if (b) b.addEventListener("click", save);
  });
  const del = document.getElementById("delete-doc");
  if (del) del.addEventListener("click", deleteCurrentEntry);

  state.lastSavedSnapshot = savedSnapshot;
  updateDirtyState();
  updatePublicLink(currentFormData());
  updateDocStats();
  refreshPreviewHeader();
  refreshPreviewBody(true);
  editor.focus({ preventScroll: true });
  // place caret at end for new docs, start for existing? end is friendlier
  placeCaretAtEnd(editor);
}

function setDocMode(mode) {
  state.docMode = mode;
  const w = document.getElementById("mode-write");
  const p = document.getElementById("mode-preview");
  const wp = document.getElementById("docs-write-pane");
  const pp = document.getElementById("docs-preview-pane");
  if (!w || !p || !wp || !pp) return;
  w.classList.toggle("active", mode === "write");
  p.classList.toggle("active", mode === "preview");
  wp.classList.toggle("hidden", mode !== "write");
  pp.classList.toggle("hidden", mode !== "preview");
  if (mode === "preview") { refreshPreviewHeader(); refreshPreviewBody(true); }
  else { const ed = document.getElementById("docs-editor"); if (ed) ed.focus({ preventScroll: true }); }
}

/* ----- toolbar ----- */
function bindToolbar(editor, onChange) {
  const bar = document.getElementById("docs-toolbar");
  if (!bar) return;
  bar.addEventListener("mousedown", (e) => {
    if (e.target.closest("button,select")) e.preventDefault(); // keep selection
  });
  bar.addEventListener("click", async (e) => {
    const btn = e.target.closest("[data-cmd]");
    if (!btn) return;
    editor.focus();
    const cmd = btn.dataset.cmd;
    await runCommand(cmd, editor, onChange);
    updateToolbarStates();
    onChange();
  });
}
async function runCommand(cmd, editor, onChange) {
  switch (cmd) {
    case "undo": document.execCommand("undo"); break;
    case "redo": document.execCommand("redo"); break;
    case "bold": document.execCommand("bold"); break;
    case "italic": document.execCommand("italic"); break;
    case "underline": document.execCommand("underline"); break;
    case "strike": document.execCommand("strikeThrough"); break;
    case "ul": document.execCommand("insertUnorderedList"); break;
    case "ol": document.execCommand("insertOrderedList"); break;
    case "quote": applyBlockStyle("blockquote"); break;
    case "codeblock": wrapCodeBlock(); break;
    case "hr": document.execCommand("insertHorizontalRule"); break;
    case "inlinecode": wrapInlineCode(); break;
    case "clear": document.execCommand("removeFormat"); document.execCommand("unlink"); applyBlockStyle("p"); break;
    case "link": {
      const sel = window.getSelection();
      const text = sel && sel.toString().slice(0, 80) || "";
      const res = await openModal({
        title: "Insert link", desc: "Link selected text to a URL.",
        fields: [
          { id: "m-text", label: "Text", value: text, placeholder: "Link text" },
          { id: "m-url", label: "URL", value: "https://", placeholder: "https://…" },
        ], confirmText: "Insert link",
      });
      if (!res) break;
      editor.focus();
      restoreSelection();
      if (!res["m-url"].trim()) break;
      if (window.getSelection().toString()) document.execCommand("createLink", false, res["m-url"].trim());
      else document.execCommand("insertHTML", false, `<a href="${escapeAttr(res["m-url"].trim())}">${escapeHtml(res["m-text"] || res["m-url"])}</a>`);
      break;
    }
    case "image": {
      const res = await openModal({
        title: "Insert image", desc: "Paste an image URL. It renders on the public post too.",
        fields: [
          { id: "m-url", label: "Image URL", value: "https://", placeholder: "https://…" },
          { id: "m-alt", label: "Alt text", value: "", placeholder: "Describe the image" },
        ], confirmText: "Insert image",
      });
      if (!res || !res["m-url"].trim()) break;
      editor.focus();
      document.execCommand("insertHTML", false, `<img src="${escapeAttr(res["m-url"].trim())}" alt="${escapeAttr(res["m-alt"] || "")}">`);
      ensureTrailingParagraph(editor);
      break;
    }
    case "table": insertTable(editor); break;
  }
  if (onChange) onChange();
}
function applyBlockStyle(tag) {
  if (tag === "pre") { wrapCodeBlock(); return; }
  if (tag === "blockquote") {
    document.execCommand("formatBlock", false, "blockquote");
    return;
  }
  document.execCommand("formatBlock", false, tag === "p" ? "p" : tag);
}
function wrapCodeBlock() {
  const sel = window.getSelection();
  if (!sel || !sel.rangeCount) return;
  const text = sel.toString() || "code";
  document.execCommand("insertHTML", false, `<pre><code>${escapeHtml(text)}</code></pre><p><br></p>`);
}
function wrapInlineCode() {
  const sel = window.getSelection();
  if (!sel || !sel.rangeCount) return;
  if (sel.isCollapsed) {
    document.execCommand("insertHTML", false, "<code>code</code>");
  } else {
    const text = sel.toString();
    document.execCommand("insertHTML", false, `<code>${escapeHtml(text)}</code>`);
  }
}
function insertTable(editor) {
  const html = `<table><thead><tr><th>Header 1</th><th>Header 2</th><th>Header 3</th></tr></thead><tbody><tr><td>…</td><td>…</td><td>…</td></tr><tr><td>…</td><td>…</td><td>…</td></tr></tbody></table><p><br></p>`;
  editor.focus();
  document.execCommand("insertHTML", false, html);
}
let savedRange = null;
function saveSelection() {
  const sel = window.getSelection();
  if (sel && sel.rangeCount) savedRange = sel.getRangeAt(0).cloneRange();
}
function restoreSelection() {
  if (!savedRange) return;
  const sel = window.getSelection();
  sel.removeAllRanges();
  sel.addRange(savedRange);
}
function updateToolbarStates() {
  const bar = document.getElementById("docs-toolbar");
  if (!bar || state.section !== "blogs") return;
  try {
    const map = { bold: "bold", italic: "italic", underline: "underline", strike: "strike", ul: "ul", ol: "ol" };
    bar.querySelectorAll("[data-cmd]").forEach((b) => {
      const c = b.dataset.cmd;
      let on = false;
      if (c === "bold") on = document.queryCommandState("bold");
      if (c === "italic") on = document.queryCommandState("italic");
      if (c === "underline") on = document.queryCommandState("underline");
      if (c === "strike") on = document.queryCommandState("strikeThrough");
      if (c === "ul") on = document.queryCommandState("insertUnorderedList");
      if (c === "ol") on = document.queryCommandState("insertOrderedList");
      b.classList.toggle("on", !!on);
    });
    const sel = document.getSelection();
    let block = "p";
    if (sel && sel.anchorNode) {
      const el = sel.anchorNode instanceof Element ? sel.anchorNode : sel.anchorNode.parentElement;
      const b = el && el.closest ? el.closest("h1,h2,h3,blockquote,pre,p") : null;
      if (b) {
        const t = b.tagName.toLowerCase();
        if (t === "h1") block = "h1";
        else if (t === "h2") block = "h2";
        else if (t === "h3") block = "h3";
        else if (t === "blockquote") block = "blockquote";
        else if (t === "pre") block = "pre";
      }
    }
    const sel2 = document.getElementById("block-style");
    if (sel2) sel2.value = block;
  } catch { /* ignore */ }
}

/* ----- slash menu ----- */
const slashItems = [
  { id: "h1", t: "Title", d: "Large page heading", ic: "H1" },
  { id: "h2", t: "Heading 1", d: "Section heading", ic: "H2" },
  { id: "h3", t: "Heading 2", d: "Sub heading", ic: "H3" },
  { id: "ul", t: "Bulleted list", d: "• item", ic: "•≡" },
  { id: "ol", t: "Numbered list", d: "1. item", ic: "1≡" },
  { id: "quote", t: "Quote", d: "Callout block", ic: "❝" },
  { id: "code", t: "Code block", d: "Fenced code", ic: "⌨" },
  { id: "image", t: "Image", d: "Embed via URL", ic: "🖼" },
  { id: "table", t: "Table", d: "3 × 3 grid", ic: "▦" },
  { id: "hr", t: "Divider", d: "Horizontal rule", ic: "―" },
];
let slashActive = 0;
function handleSlashTrigger(editor) {
  const sel = window.getSelection();
  if (!sel || !sel.rangeCount || !sel.isCollapsed) { hideSlashMenu(); return; }
  const node = sel.anchorNode;
  const text = node && node.textContent ? node.textContent.slice(0, sel.anchorOffset) : "";
  const m = /(^|\s)\/([a-z]*)$/i.exec(text || "");
  if (!m) { if (!dom.slashMenu.classList.contains("hidden")) { /* keep while navigating? */ } else hideSlashMenu(); return; }
  showSlashMenu(m[2] || "", editor);
}
function showSlashMenu(filter, editor) {
  const list = slashItems.filter((x) => x.t.toLowerCase().includes(filter.toLowerCase()));
  if (!list.length) { hideSlashMenu(); return; }
  slashActive = 0;
  dom.slashMenu.innerHTML = list.map((x, i) =>
    `<button type="button" class="slash-item${i === 0 ? " active" : ""}" data-id="${x.id}"><span class="ic">${x.ic}</span><span><b>${x.t}</b><small>${x.d}</small></span></button>`).join("");
  dom.slashMenu.classList.remove("hidden");
  const sel = window.getSelection();
  let x = 100, y = 200;
  if (sel && sel.rangeCount) { const r = sel.getRangeAt(0).getBoundingClientRect(); x = r.left; y = r.bottom + 8; }
  dom.slashMenu.style.left = Math.min(window.innerWidth - 300, Math.max(10, x)) + "px";
  dom.slashMenu.style.top = Math.min(window.innerHeight - 360, y) + window.scrollY * 0 + "px";
  dom.slashMenu.style.position = "fixed";
  dom.slashMenu.querySelectorAll(".slash-item").forEach((b) => {
    b.addEventListener("mousedown", (e) => e.preventDefault());
    b.addEventListener("click", async () => {
      const id = b.dataset.id;
      deleteSlashTrigger(editor);
      await insertSlashBlock(id, editor);
    });
  });
  dom.slashMenu.dataset.items = JSON.stringify(list.map((x) => x.id));
}
function hideSlashMenu() { if (dom.slashMenu) dom.slashMenu.classList.add("hidden"); }
function deleteSlashTrigger(editor) {
  const sel = window.getSelection();
  if (!sel || !sel.rangeCount) return;
  const node = sel.anchorNode;
  if (node && node.nodeType === 3) {
    const off = sel.anchorOffset;
    const txt = node.textContent;
    const slashIdx = txt.lastIndexOf("/", off - 1);
    if (slashIdx >= 0) {
      node.textContent = txt.slice(0, slashIdx) + txt.slice(off);
      const r = document.createRange();
      r.setStart(node, slashIdx); r.collapse(true);
      sel.removeAllRanges(); sel.addRange(r);
    }
  }
  hideSlashMenu();
}
async function insertSlashBlock(id, editor) {
  editor.focus();
  switch (id) {
    case "h1": document.execCommand("formatBlock", false, "h1"); break;
    case "h2": document.execCommand("formatBlock", false, "h2"); break;
    case "h3": document.execCommand("formatBlock", false, "h3"); break;
    case "ul": document.execCommand("insertUnorderedList"); break;
    case "ol": document.execCommand("insertOrderedList"); break;
    case "quote": document.execCommand("formatBlock", false, "blockquote"); break;
    case "code": document.execCommand("insertHTML", false, "<pre><code>code</code></pre><p><br></p>"); break;
    case "hr": document.execCommand("insertHorizontalRule"); break;
    case "table": insertTable(editor); break;
    case "image": await runCommand("image", editor, null); break;
  }
  updateDirtyState();
  persistBlogAutosave();
  updateDocStats();
}

/* ----- markdown shortcuts / keys / paste ----- */
function currentBlock() {
  const sel = window.getSelection();
  if (!sel || !sel.anchorNode) return null;
  const el = sel.anchorNode instanceof Element ? sel.anchorNode : sel.anchorNode.parentElement;
  return el && el.closest ? el.closest("p,h1,h2,h3,blockquote,pre,li") : null;
}
function handleMarkdownShortcuts(editor) {
  const b = currentBlock();
  if (!b || b.tagName === "PRE" || b.tagName === "LI") return;
  if (!editor.contains(b)) return;
  const txt = b.textContent || "";
  const apply = (tag) => {
    b.textContent = txt.replace(/^(#{1,3}|>\s?|-\s?|\*\s?|1\.\s?)/, "").replace(/^```/, "");
    document.execCommand("formatBlock", false, tag === "p" ? "p" : tag);
    if (tag === "ul") document.execCommand("insertUnorderedList");
    if (tag === "ol") document.execCommand("insertOrderedList");
  };
  if (/^#\s/.test(txt)) { apply("h1"); return; }
  if (/^##\s/.test(txt)) { apply("h2"); return; }
  if (/^###\s/.test(txt)) { apply("h3"); return; }
  if (/^>\s/.test(txt)) { apply("blockquote"); return; }
  if (/^(-|\*)\s/.test(txt)) { b.textContent = txt.replace(/^(-|\*)\s/, ""); document.execCommand("insertUnorderedList"); return; }
  if (/^1\.\s/.test(txt)) { b.textContent = txt.replace(/^1\.\s/, ""); document.execCommand("insertOrderedList"); return; }
  if (/^```/.test(txt)) { b.textContent = txt.replace(/^```/, ""); wrapCodeBlock(); return; }
  if (/^---\s*$/.test(txt) && b.tagName === "P") { b.textContent = ""; document.execCommand("insertHorizontalRule"); return; }
}
function handleEditorKeys(e, editor, onChange) {
  if (e.key === "Enter" && !dom.slashMenu.classList.contains("hidden")) {
    const items = dom.slashMenu.querySelectorAll(".slash-item");
    const active = items[slashActive] || items[0];
    if (active) { e.preventDefault(); active.click(); return; }
  }
  if (e.key === "ArrowDown" && !dom.slashMenu.classList.contains("hidden")) {
    e.preventDefault();
    slashActive = Math.min(dom.slashMenu.querySelectorAll(".slash-item").length - 1, slashActive + 1);
    highlightSlash();
    return;
  }
  if (e.key === "ArrowUp" && !dom.slashMenu.classList.contains("hidden")) {
    e.preventDefault();
    slashActive = Math.max(0, slashActive - 1);
    highlightSlash();
    return;
  }
  // keep code readable: tab inserts two spaces inside pre
  if (e.key === "Tab") {
    const b = currentBlock();
    if (b && (b.tagName === "PRE" || b.closest("pre"))) { e.preventDefault(); document.execCommand("insertText", false, "  "); }
  }
}
function highlightSlash() {
  dom.slashMenu.querySelectorAll(".slash-item").forEach((b, i) => b.classList.toggle("active", i === slashActive));
}
function handlePaste(e, editor, onChange) {
  e.preventDefault();
  const html = (e.clipboardData || window.clipboardData).getData("text/html");
  const text = (e.clipboardData || window.clipboardData).getData("text/plain");
  if (html && window.DOMPurify) {
    const clean = window.DOMPurify.sanitize(html, { ALLOWED_TAGS: ["p", "h1", "h2", "h3", "strong", "b", "em", "i", "u", "s", "a", "ul", "ol", "li", "blockquote", "code", "pre", "img", "hr", "table", "thead", "tbody", "tr", "th", "td", "br"], ALLOWED_ATTR: ["href", "src", "alt", "colspan", "rowspan"] });
    document.execCommand("insertHTML", false, clean);
  } else if (text) {
    // multi-line paste -> paragraphs
    const esc = escapeHtml(text).replace(/\n\n+/g, "</p><p>").replace(/\n/g, "<br>");
    document.execCommand("insertHTML", false, `<p>${esc}</p>`);
  }
  ensureTrailingParagraph(editor);
  onChange();
}
function ensureTrailingParagraph(editor) {
  if (!editor.lastElementChild || !/^P|H1|H2|H3|UL|OL|TABLE|PRE|BLOCKQUOTE|HR$/.test(editor.lastElementChild.tagName)) return;
  if (editor.lastElementChild.tagName !== "P") {
    const p = document.createElement("p");
    p.innerHTML = "<br>";
    editor.appendChild(p);
  }
}
function placeCaretAtEnd(el) {
  try {
    const r = document.createRange();
    r.selectNodeContents(el);
    r.collapse(false);
    const s = window.getSelection();
    s.removeAllRanges(); s.addRange(r);
  } catch { /* ignore */ }
}

/* ----- markdown <-> html ----- */
function initTurndown() {
  try {
    if (!window.TurndownService) return;
    const td = new window.TurndownService({ headingStyle: "atx", codeBlockStyle: "fenced", hr: "---", bulletListMarker: "-" });
    if (window.turndownPluginGfm) {
      const gfm = window.turndownPluginGfm;
      if (gfm.gfm) td.use(gfm.gfm);
      else if (Array.isArray(gfm)) td.use(gfm[0]);
      else Object.values(gfm).forEach((p) => { try { td.use(p); } catch {} });
    }
    td.addRule("underline", { filter: ["u"], replacement: (c) => `<u>${c}</u>` });
    td.addRule("strikethrough", { filter: ["s", "strike", "del"], replacement: (c) => `~~${c}~~` });
    state.turndown = td;
  } catch { state.turndown = null; }
}
function markdownToHtml(md) {
  try {
    let html = "";
    if (window.marked && window.marked.parse) html = window.marked.parse(md || "", { gfm: true, breaks: false });
    else html = `<p>${escapeHtml(md || "")}</p>`;
    if (window.DOMPurify) html = window.DOMPurify.sanitize(html);
    return html || "<p><br></p>";
  } catch { return `<p>${escapeHtml(md || "")}</p>`; }
}
function htmlToMarkdown(html) {
  const tmp = document.createElement("div");
  tmp.innerHTML = html || "";
  // remove empty trailing breaks artefacts
  const text = tmp.textContent || "";
  if (!text.trim() && !tmp.querySelector("img,table,hr,pre")) return "";
  try {
    if (state.turndown) {
      let md = state.turndown.turndown(html || "");
      return (md || "").replace(/\n{3,}/g, "\n\n").trim();
    }
  } catch { /* fallback */ }
  return text.trim();
}
function editorMarkdown() {
  const ed = document.getElementById("docs-editor");
  if (!ed) return "";
  return htmlToMarkdown(ed.innerHTML);
}

/* ----- blog form data / stats / preview ----- */
function currentFormData() {
  if (state.section === "blogs") {
    return {
      title: valueOf("doc-title") || valueOf("title"),
      slug: valueOf("f-slug") || valueOf("slug"),
      category: valueOf("f-category") || valueOf("category"),
      status: normalizeBlogStatus(valueOf("f-status") || valueOf("status") || "draft"),
      thumbnail: valueOf("f-thumbnail") || valueOf("thumbnail"),
      description: valueOf("f-description") || valueOf("description"),
      markdown: editorMarkdown() || valueOf("markdown-editor"),
    };
  }
  if (state.section === "projects") {
    return {
      title: valueOf("title"), thumbnail: valueOf("thumbnail"),
      date_start: valueOf("date_start"), date_end: valueOf("date_end"),
      tags: normalizeTags(state.tags), description: valueOf("description"),
      github_link: valueOf("github_link"), website_link: valueOf("website_link"),
    };
  }
  return { title: valueOf("title"), video_url: valueOf("video_url"), description: valueOf("description") };
}
function updateDocStats() {
  const ed = document.getElementById("docs-editor");
  const title = valueOf("doc-title");
  const bodyText = ed ? (ed.innerText || "") : "";
  const words = (title + " " + bodyText).trim().split(/\s+/).filter(Boolean).length;
  const chars = bodyText.length;
  const md = editorMarkdown();
  const read = Math.max(1, Math.ceil(words / 200));
  setText("stat-words", `${words} word${words === 1 ? "" : "s"}`);
  setText("stat-chars", `${chars} chars`);
  setText("stat-read", `${read} min read`);
  setText("stat-md", `${md.length} md chars`);
  setText("doc-stats-top", `${words} words · ${read} min`);
  const note = document.getElementById("autosave-note");
  if (note) note.textContent = state.dirty ? "editing…" : "saved";
}
function refreshPreviewHeader() {
  const d = currentFormData();
  setText("pv-cat", d.category || "draft preview");
  setText("pv-title", d.title || "Untitled post");
  setText("pv-desc", d.description || "Add a description to see the intro.");
  const meta = [];
  if (state.blogDates.datePublished) meta.push(state.blogDates.datePublished);
  if (state.blogDates.lastUpdated) meta.push("Updated " + state.blogDates.lastUpdated);
  setText("pv-meta", meta.length ? meta.join(" • ") : "Dates assign automatically on save.");
  const img = document.getElementById("pv-thumb");
  if (img) {
    if (d.thumbnail) { img.src = d.thumbnail; img.style.display = ""; }
    else img.style.display = "none";
  }
}
function schedulePreviewRefresh() {
  window.clearTimeout(state.previewTimer);
  state.previewTimer = window.setTimeout(() => refreshPreviewBody(false), 350);
  refreshPreviewHeader();
}
async function refreshPreviewBody(immediate) {
  const body = document.getElementById("pv-body");
  if (!body) return;
  const req = ++state.previewRequestId;
  const md = editorMarkdown();
  if (!md.trim()) { body.innerHTML = `<p class="muted">Nothing to preview yet — start writing.</p>`; return; }
  // debounce unless immediate
  if (!immediate) await new Promise((r) => setTimeout(r, 120));
  if (req !== state.previewRequestId) return;
  try {
    const res = await fetchJSON("/api/admin/markdown/render", { method: "POST", body: JSON.stringify({ markdown: md }) });
    if (req !== state.previewRequestId) return;
    body.innerHTML = res.html || "";
  } catch {
    // fallback to local marked render
    if (req !== state.previewRequestId) return;
    body.innerHTML = markdownToHtml(md);
  }
}

/* ----- autosave ----- */
function blogAutosaveKey(item) { const id = getItemId(item); return `admin:blog-autosave:${id || "new"}`; }
function persistBlogAutosave() {
  if (state.section !== "blogs") return;
  try { window.localStorage.setItem(blogAutosaveKey({ id: state.selectedId }), JSON.stringify(currentFormData())); } catch {}
}
function restoreAutosave(key) {
  try { const raw = window.localStorage.getItem(key); return raw ? JSON.parse(raw) : null; } catch { return null; }
}
function clearAutosaveForCurrentBlog() {
  if (state.section !== "blogs") return;
  try {
    window.localStorage.removeItem(blogAutosaveKey({ id: state.selectedId }));
    window.localStorage.removeItem("admin:blog-autosave:new");
  } catch {}
}
function hydrateBlogForm(d) {
  const map = { title: "doc-title", slug: "f-slug", category: "f-category", status: "f-status", thumbnail: "f-thumbnail", description: "f-description" };
  Object.entries(map).forEach(([k, id]) => {
    const el = document.getElementById(id);
    if (el && typeof d[k] === "string") el.value = d[k];
  });
  if (typeof d.markdown === "string") {
    const ed = document.getElementById("docs-editor");
    if (ed) ed.innerHTML = markdownToHtml(d.markdown);
  }
  updateDocStats();
  refreshPreviewHeader();
}

/* ================= PROJECTS / PIECES ================= */
function renderProjectEditor(item) {
  return `
  <form id="entity-form" class="form-shell">
    <div class="form-head"><p class="eyebrow">${state.mode === "edit" ? "Editing project" : "New project"}</p>
      <h3>${escapeHtml(item.title || "Untitled project")}</h3>
      <p>Cards, order and links — exactly what the public projects grid reads.</p></div>
    <div class="form-grid">
      <div class="field"><label for="title">Title</label><input id="title" value="${escapeAttr(item.title || "")}" required></div>
      <div class="field"><label for="thumbnail">Thumbnail URL</label><input id="thumbnail" value="${escapeAttr(item.thumbnail || "")}" placeholder="https://…"></div>
      <div class="field"><label for="date_start">Start date</label><input id="date_start" value="${escapeAttr(item.date_start || "")}" placeholder="January 2024"></div>
      <div class="field"><label for="date_end">End date</label><input id="date_end" value="${escapeAttr(item.date_end || "")}" placeholder="March 2024"></div>
      <div class="field field-span-2"><label>Tags</label>
        <div class="chip-editor"><div id="tag-list" class="chip-list"></div>
        <input id="tag-input" class="tag-input" placeholder="Add a tag + Enter"></div></div>
      <div class="field field-span-2"><label>Thumbnail preview</label>
        <div class="thumb-row"><img id="proj-thumb" src="${escapeAttr(item.thumbnail || "")}" style="${item.thumbnail ? "" : "display:none"}"></div></div>
      <div class="field field-span-2"><label for="description">Description</label><textarea id="description" required>${escapeHtml(item.description || "")}</textarea></div>
      <div class="field"><label for="github_link">GitHub link</label><input id="github_link" value="${escapeAttr(item.github_link || "")}" placeholder="https://github.com/…"></div>
      <div class="field"><label for="website_link">Website link</label><input id="website_link" value="${escapeAttr(item.website_link || "")}" placeholder="https://…"></div>
    </div>
    <div class="docs-footer" style="padding:0">
      <div class="docs-footer-left"><button type="submit" class="primary-button">Save project</button>
      ${state.mode === "edit" ? `<button type="button" id="delete-entry" class="danger-button">Delete</button>` : ""}</div>
      <span class="save-hint mono">⌘S saves · drag list to reorder</span>
    </div>
  </form>`;
}
function renderPieceEditor(item) {
  return `
  <form id="entity-form" class="form-shell">
    <div class="form-head"><p class="eyebrow">${state.mode === "edit" ? "Editing piece" : "New piece"}</p>
      <h3>${escapeHtml(item.title || "Untitled piece")}</h3>
      <p>Recordings, demos, hosted pieces.</p></div>
    <div class="form-grid">
      <div class="field"><label for="title">Title</label><input id="title" value="${escapeAttr(item.title || "")}" required></div>
      <div class="field"><label for="video_url">Video URL</label><input id="video_url" value="${escapeAttr(item.video_url || "")}" placeholder="https://youtube.com/…"></div>
      <div class="field field-span-2"><label for="description">Description</label><textarea id="description" required>${escapeHtml(item.description || "")}</textarea></div>
      <div class="field field-span-2"><span class="field-label">Link check</span>
        <div class="card-box mono" style="font-size:.88rem"><a id="piece-link" href="${escapeAttr(item.video_url || "#")}" target="_blank" rel="noreferrer" style="color:var(--color-teal)">${escapeHtml(item.video_url || "Add a URL to preview the link")}</a></div></div>
    </div>
    <div class="docs-footer" style="padding:0">
      <div class="docs-footer-left"><button type="submit" class="primary-button">Save piece</button>
      ${state.mode === "edit" ? `<button type="button" id="delete-entry" class="danger-button">Delete</button>` : ""}</div>
      <span class="save-hint mono">⌘S saves</span>
    </div>
  </form>`;
}
function bindProjectEditor(item) {
  const form = document.getElementById("entity-form");
  renderTags();
  bindStandard(form, updateDirtyState);
  const tagInput = document.getElementById("tag-input");
  tagInput.addEventListener("keydown", (e) => {
    if (e.key !== "Enter" && e.key !== ",") return;
    e.preventDefault();
    const v = tagInput.value.trim();
    if (!v) return;
    state.tags.push(v);
    state.tags = normalizeTags(state.tags);
    tagInput.value = "";
    renderTags();
    updateDirtyState();
  });
  const thumb = document.getElementById("thumbnail");
  const img = document.getElementById("proj-thumb");
  thumb.addEventListener("input", () => {
    if (thumb.value.trim()) { img.src = thumb.value.trim(); img.style.display = ""; }
    else img.style.display = "none";
  });
  form.addEventListener("submit", (e) => { e.preventDefault(); saveCurrentEntry(); });
  bindDeleteButton();
  state.lastSavedSnapshot = snapshotForState(currentFormData());
  setDirty(false);
}
function bindPieceEditor() {
  const form = document.getElementById("entity-form");
  bindStandard(form, updateDirtyState);
  const url = document.getElementById("video_url");
  const link = document.getElementById("piece-link");
  if (url && link) url.addEventListener("input", () => {
    link.href = url.value.trim() || "#";
    link.textContent = url.value.trim() || "Add a URL to preview the link";
  });
  form.addEventListener("submit", (e) => { e.preventDefault(); saveCurrentEntry(); });
  bindDeleteButton();
  state.lastSavedSnapshot = snapshotForState(currentFormData());
  setDirty(false);
}
function bindStandard(form, onChange) {
  Array.from(form.querySelectorAll("input,textarea,select")).forEach((el) => {
    el.addEventListener("input", onChange);
    el.addEventListener("change", onChange);
  });
}
function bindDeleteButton() {
  const b = document.getElementById("delete-entry");
  if (b) b.addEventListener("click", deleteCurrentEntry);
}
function renderTags() {
  const list = document.getElementById("tag-list");
  if (!list) return;
  list.innerHTML = "";
  state.tags.forEach((tag, i) => {
    const chip = document.createElement("div");
    chip.className = "chip";
    chip.innerHTML = `<span>${escapeHtml(tag)}</span><button type="button" aria-label="Remove">×</button>`;
    chip.querySelector("button").addEventListener("click", () => { state.tags.splice(i, 1); renderTags(); updateDirtyState(); });
    list.appendChild(chip);
  });
}

/* ================= SAVE / DELETE ================= */
async function saveCurrentEntry() {
  if (state.saveInFlight) return;
  const payload = currentFormData();
  const meta = sectionMeta[state.section];

  if (state.section === "blogs") {
    let ok = true;
    ok = requireField("doc-title", "err-title", payload.title, "Title is required.") && ok;
    ok = requireField("f-slug", "err-slug", payload.slug, "Slug is required.") && ok;
    ok = requireField("f-category", "err-category", payload.category, "Category is required.") && ok;
    ok = requireField("f-thumbnail", "err-thumbnail", payload.thumbnail, "Cover image is required.") && ok;
    ok = requireField("f-description", "err-description", payload.description, "Description is required.") && ok;
    if (!payload.markdown || !payload.markdown.trim()) {
      toast("Write something in the doc first.", true);
      const ed = document.getElementById("docs-editor");
      if (ed) ed.focus();
      ok = false;
    }
    if (!/^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(payload.slug || "")) {
      showFieldError("err-slug", "Use lowercase letters, numbers, dashes.");
      ok = false;
    }
    if (!ok) {
      const s = document.getElementById("doc-settings");
      if (s && !s.open) s.open = true;
      return;
    }
  }
  if (state.section === "projects" && (!payload.title || !payload.description)) { toast("Title + description required.", true); return; }
  if (state.section === "pieces" && (!payload.title || !payload.description)) { toast("Title + description required.", true); return; }

  const method = state.mode === "edit" && state.selectedId ? "PUT" : "POST";
  const url = method === "POST" ? meta.endpoint : `${meta.endpoint}/${state.selectedId}`;
  state.saveInFlight = true;
  setStatusOverride("Saving…");
  try {
    const res = await fetchJSON(url, { method, body: JSON.stringify(payload) });
    clearAutosaveForCurrentBlog();
    setDirty(false);
    await refreshCounts();
    const id = (res && res.document && res.document.id) || (res && res.project && getItemId(res.project)) || (res && res.piece && getItemId(res.piece)) || (method === "POST" ? null : state.selectedId);
    toast(state.section === "blogs" ? "Post saved" : "Saved");
    if (id) await loadCurrentSection(id);
    else await loadCurrentSection();
  } catch (err) {
    toast(err.message || "Save failed.", true);
  } finally {
    state.saveInFlight = false;
    setStatusOverride("");
  }
}
async function deleteCurrentEntry() {
  if (!state.selectedId) return;
  const meta = sectionMeta[state.section];
  const ok = await openModal({
    title: `Delete this ${meta.singular.toLowerCase()}?`,
    desc: "This cannot be undone. The public page and sitemap update immediately.",
    fields: [], confirmText: "Delete", danger: true,
  });
  if (!ok) return;
  try {
    await fetchJSON(`${meta.endpoint}/${state.selectedId}`, { method: "DELETE" });
    clearAutosaveForCurrentBlog();
    toast("Deleted");
    await refreshCounts();
    await loadCurrentSection();
  } catch (err) { toast(err.message || "Delete failed.", true); }
}
function requireField(inputId, errId, val, msg) {
  if (val && String(val).trim()) { clearFieldError(errId); return true; }
  showFieldError(errId, msg);
  return false;
}
function showFieldError(id, msg) {
  const el = document.getElementById(id);
  if (!el) { if (msg) toast(msg, true); return; }
  el.textContent = msg;
  el.classList.remove("hidden");
}
function clearFieldError(id) { const el = document.getElementById(id); if (el) el.classList.add("hidden"); }

/* ================= dirty / status / public link ================= */
function updateDirtyState() { setDirty(snapshotForState(currentFormData()) !== state.lastSavedSnapshot); }
function setDirty(d) { state.dirty = d; renderSaveStatus(); }
function setStatusOverride(msg) { state.statusOverride = msg || ""; renderSaveStatus(); }
function renderSaveStatus() {
  if (state.statusOverride) {
    dom.saveStatus.textContent = state.statusOverride;
    dom.saveStatus.classList.remove("is-dirty");
    dom.saveStatus.classList.add("is-saving");
    return;
  }
  dom.saveStatus.textContent = state.saveInFlight ? "Saving…" : state.dirty ? "Unsaved changes" : "Saved";
  dom.saveStatus.classList.toggle("is-dirty", state.dirty);
  dom.saveStatus.classList.remove("is-saving");
  const note = document.getElementById("autosave-note");
  if (note) note.textContent = state.dirty ? "editing…" : "saved";
}
function confirmDiscardIfNeeded() {
  if (!state.dirty) return true;
  return window.confirm("You have unsaved changes. Discard them?");
}
function updatePublicLink(item) {
  if (state.section !== "blogs" || !item || normalizeBlogStatus(item.status) !== "published" || !item.slug) {
    dom.publicLink.classList.add("hidden");
    return;
  }
  dom.publicLink.classList.remove("hidden");
  dom.publicLink.setAttribute("href", `/blog/${encodeURIComponent(item.slug)}`);
}

/* ================= modal / toast ================= */
let modalResolve = null;
function openModal({ title, desc, fields = [], confirmText = "Confirm", danger = false }) {
  dom.modalTitle.textContent = title || "Dialog";
  dom.modalDesc.textContent = desc || "";
  dom.modalFields.innerHTML = "";
  const inputs = {};
  fields.forEach((f) => {
    const label = document.createElement("label");
    label.textContent = f.label || "";
    const input = document.createElement("input");
    input.id = f.id;
    input.value = f.value || "";
    input.placeholder = f.placeholder || "";
    label.appendChild(input);
    dom.modalFields.appendChild(label);
    inputs[f.id] = input;
  });
  dom.modalConfirm.textContent = confirmText;
  dom.modalConfirm.className = danger ? "danger-button" : "primary-button";
  dom.modalRoot.classList.remove("hidden");
  saveSelection();
  const first = dom.modalFields.querySelector("input");
  if (first) setTimeout(() => first.focus(), 30);
  else setTimeout(() => dom.modalConfirm.focus(), 30);
  return new Promise((resolve) => {
    modalResolve = resolve;
    dom.modalConfirm.onclick = () => {
      const out = {};
      Object.entries(inputs).forEach(([k, el]) => (out[k] = el.value));
      closeModal(fields.length ? out : true);
    };
    dom.modalCancel.onclick = () => closeModal(fields.length ? null : false);
    dom.modalRoot.querySelector("[data-close-modal]").onclick = () => closeModal(fields.length ? null : false);
  });
}
function closeModal(val) {
  dom.modalRoot.classList.add("hidden");
  if (modalResolve) { modalResolve(val); modalResolve = null; }
  restoreSelection();
}
function toast(msg, isErr) {
  const t = document.createElement("div");
  t.className = "toast" + (isErr ? " error" : "");
  t.textContent = msg;
  dom.toastRoot.appendChild(t);
  setTimeout(() => { t.style.opacity = "0"; t.style.transition = "opacity .3s"; }, 2600);
  setTimeout(() => t.remove(), 3000);
}

/* ================= utils ================= */
async function fetchJSON(url, options = {}) {
  const res = await fetch(url, {
    credentials: "same-origin",
    headers: { "Content-Type": "application/json", ...(options.headers || {}) },
    ...options,
  });
  const text = await res.text();
  let payload = null;
  try { payload = text ? JSON.parse(text) : null; } catch { payload = null; }
  if (!res.ok) {
    const msg = payload && payload.error ? payload.error : text || `Request failed (${res.status})`;
    throw new Error(msg);
  }
  return payload;
}
function snapshotForState(d) { return JSON.stringify(d || {}); }
function valueOf(id) { const el = document.getElementById(id); return el ? el.value.trim() : ""; }
function setText(id, v) { const el = document.getElementById(id); if (el) el.textContent = v; }
function defaultItemForSection(s) {
  if (s === "blogs") return { title: "", slug: "", category: "", status: "draft", thumbnail: "", description: "", markdown: "" };
  if (s === "projects") return { title: "", thumbnail: "", date_start: "", date_end: "", tags: [], description: "", github_link: "", website_link: "" };
  return { title: "", video_url: "", description: "" };
}
function normalizeBlogStatus(s) { return String(s || "").trim().toLowerCase() === "draft" ? "draft" : "published"; }
function normalizeTags(tags) { return [...new Set((Array.isArray(tags) ? tags : []).map((t) => String(t).trim()).filter(Boolean))]; }
function getItemId(item) {
  if (!item || item.id == null) return null;
  if (typeof item.id === "string") return item.id;
  if (typeof item.id === "object" && typeof item.id.$oid === "string") return item.id.$oid;
  return String(item.id);
}
function escapeHtml(v) {
  return String(v || "").replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;").replaceAll('"', "&quot;").replaceAll("'", "&#39;");
}
function escapeAttr(v) { return escapeHtml(v); }
function truncate(s, n) { s = String(s || ""); return s.length > n ? s.slice(0, n - 1) + "…" : s; }
function slugify(v) {
  return String(v || "").trim().toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "");
}
function showLoginError(m) { dom.loginError.textContent = m; dom.loginError.classList.remove("hidden"); }
function hideLoginError() { dom.loginError.textContent = ""; dom.loginError.classList.add("hidden"); }
