const state = {
    section: "blogs",
    items: [],
    selectedId: null,
    mode: "new",
    dirty: false,
    lastSavedSnapshot: "",
    tags: [],
    editor: null,
    listQuery: "",
    slugTouched: false,
    previewRequestId: 0,
    previewTimer: null,
};

const dom = {
    loginView: document.getElementById("login-view"),
    appView: document.getElementById("app-view"),
    loginForm: document.getElementById("login-form"),
    passwordInput: document.getElementById("password"),
    loginError: document.getElementById("login-error"),
    navLinks: Array.from(document.querySelectorAll(".nav-link")),
    createEntryButton: document.getElementById("create-entry"),
    logoutButton: document.getElementById("logout-button"),
    saveStatus: document.getElementById("save-status"),
    publicLink: document.getElementById("public-link"),
    sectionKicker: document.getElementById("section-kicker"),
    sectionTitle: document.getElementById("section-title"),
    listHeading: document.getElementById("list-heading"),
    listSearch: document.getElementById("list-search"),
    entryList: document.getElementById("entry-list"),
    listEmpty: document.getElementById("list-empty"),
    editorHeading: document.getElementById("editor-heading"),
    editorRoot: document.getElementById("editor-root"),
};

const sectionMeta = {
    blogs: {
        label: "Blogs",
        singular: "Blog Post",
        newLabel: "New Blog Post",
        endpoint: "/api/admin/blogs",
    },
    projects: {
        label: "Projects",
        singular: "Project",
        newLabel: "New Project",
        endpoint: "/api/admin/projects",
    },
    pieces: {
        label: "Pieces",
        singular: "Piece",
        newLabel: "New Piece",
        endpoint: "/api/admin/pieces",
    },
};

document.addEventListener("DOMContentLoaded", () => {
    bindGlobalEvents();
    boot();
});

function bindGlobalEvents() {
    dom.loginForm.addEventListener("submit", handleLogin);
    dom.logoutButton.addEventListener("click", handleLogout);
    dom.createEntryButton.addEventListener("click", () => startNewEntry());
    dom.listSearch.addEventListener("input", () => {
        state.listQuery = dom.listSearch.value.trim().toLowerCase();
        renderList();
    });

    dom.navLinks.forEach((button) => {
        button.addEventListener("click", async () => {
            const nextSection = button.dataset.section;
            if (nextSection === state.section) {
                return;
            }
            if (!confirmDiscardIfNeeded()) {
                return;
            }
            await setSection(nextSection);
        });
    });
}

async function boot() {
    const authenticated = await checkAuthentication();
    const onLoginRoute = window.location.pathname === "/admin/login";

    if (!authenticated) {
        showLogin();
        if (!onLoginRoute) {
            window.location.replace("/admin/login");
        }
        return;
    }

    if (onLoginRoute) {
        window.location.replace("/admin");
        return;
    }

    showApp();
    await setSection(state.section);
}

async function checkAuthentication() {
    try {
        await fetchJSON("/api/admin/me");
        return true;
    } catch (error) {
        return false;
    }
}

function showLogin() {
    dom.loginView.classList.remove("hidden");
    dom.appView.classList.add("hidden");
}

function showApp() {
    dom.loginView.classList.add("hidden");
    dom.appView.classList.remove("hidden");
}

async function handleLogin(event) {
    event.preventDefault();
    hideLoginError();

    try {
        await fetchJSON("/api/admin/login", {
            method: "POST",
            body: JSON.stringify({ password: dom.passwordInput.value }),
        });
        window.location.replace("/admin");
    } catch (error) {
        showLoginError(error.message || "Login failed.");
    }
}

async function handleLogout() {
    try {
        await fetchJSON("/api/admin/logout", { method: "POST" });
    } finally {
        window.location.replace("/admin/login");
    }
}

async function setSection(sectionName) {
    destroyEditor();
    state.section = sectionName;
    state.items = [];
    state.selectedId = null;
    state.mode = "new";
    state.tags = [];
    state.listQuery = "";
    state.slugTouched = false;
    dom.listSearch.value = "";
    updateSectionChrome();
    await loadCurrentSection();
}

function updateSectionChrome() {
    const meta = sectionMeta[state.section];
    dom.navLinks.forEach((button) => {
        button.classList.toggle("active", button.dataset.section === state.section);
    });
    dom.sectionKicker.textContent = meta.label;
    dom.sectionTitle.textContent = `Manage ${meta.label}`;
    dom.listHeading.textContent = `All ${meta.label}`;
    dom.editorHeading.textContent = meta.newLabel;
    dom.createEntryButton.textContent = meta.newLabel;
    setDirty(false);
    updatePublicLink(null);
}

async function loadCurrentSection(preferSelectionId) {
    const meta = sectionMeta[state.section];
    dom.entryList.innerHTML = "";
    dom.listEmpty.textContent = `Loading ${meta.label.toLowerCase()}...`;
    dom.listEmpty.classList.remove("hidden");

    try {
        const items = await fetchJSON(meta.endpoint);
        state.items = Array.isArray(items) ? items : [];
        renderList();

        if (preferSelectionId) {
            const exists = state.items.find((item) => getItemId(item) === preferSelectionId);
            if (exists) {
                await selectEntry(preferSelectionId);
                return;
            }
        }

        if (state.items.length > 0) {
            await selectEntry(getItemId(state.items[0]));
            return;
        }

        startNewEntry({ skipConfirm: true });
    } catch (error) {
        dom.entryList.innerHTML = "";
        dom.listEmpty.textContent = error.message || `Failed to load ${meta.label.toLowerCase()}.`;
        dom.listEmpty.classList.remove("hidden");
        startNewEntry({ skipConfirm: true });
    }
}

function filteredItems() {
    if (!state.listQuery) {
        return state.items;
    }

    return state.items.filter((item) => {
        const haystack = [
            item.title,
            item.slug,
            item.description,
            item.category,
            item.date_start,
            item.date_end,
            item.video_url,
            Array.isArray(item.tags) ? item.tags.join(" ") : "",
        ]
            .filter(Boolean)
            .join(" ")
            .toLowerCase();
        return haystack.includes(state.listQuery);
    });
}

function renderList() {
    const items = filteredItems();
    dom.entryList.innerHTML = "";

    if (items.length === 0) {
        dom.listEmpty.textContent = state.listQuery ? "No matching entries." : "Nothing here yet.";
        dom.listEmpty.classList.remove("hidden");
        return;
    }

    dom.listEmpty.classList.add("hidden");

    items.forEach((item) => {
        const card = document.createElement("button");
        card.type = "button";
        card.className = "entry-card";
        card.classList.toggle("active", getItemId(item) === state.selectedId);
        card.innerHTML = renderListCard(item);
        card.addEventListener("click", async () => {
            const id = getItemId(item);
            if (id === state.selectedId) {
                return;
            }
            if (!confirmDiscardIfNeeded()) {
                return;
            }
            await selectEntry(id);
        });
        dom.entryList.appendChild(card);
    });
}

function renderListCard(item) {
    if (state.section === "blogs") {
        const status = normalizeBlogStatus(item.status || "published");
        return `
            <h4>${escapeHtml(item.title || "Untitled post")}</h4>
            <div class="entry-meta">
                <span class="badge badge-${status}">${escapeHtml(status)}</span>
                <span>${escapeHtml(item.category || "Uncategorized")}</span>
                <span>${escapeHtml(item.date_published || "No publish date")}</span>
            </div>
            <p class="entry-snippet">${escapeHtml(item.description || "No description yet.")}</p>
        `;
    }

    if (state.section === "projects") {
        const dateParts = [item.date_start, item.date_end].filter(Boolean).join(" - ");
        const tagText = Array.isArray(item.tags) && item.tags.length > 0 ? item.tags.join(", ") : "No tags";
        return `
            <h4>${escapeHtml(item.title || "Untitled project")}</h4>
            <div class="entry-meta">
                <span>${escapeHtml(dateParts || "No dates")}</span>
            </div>
            <p class="entry-snippet">${escapeHtml(item.description || tagText)}</p>
        `;
    }

    return `
        <h4>${escapeHtml(item.title || "Untitled piece")}</h4>
        <div class="entry-meta">
            <span>${escapeHtml(item.video_url || "No video URL")}</span>
        </div>
        <p class="entry-snippet">${escapeHtml(item.description || "No description yet.")}</p>
    `;
}

async function selectEntry(id) {
    destroyEditor();
    state.selectedId = id;
    state.mode = "edit";
    renderList();

    const meta = sectionMeta[state.section];
    dom.editorHeading.textContent = `Loading ${meta.singular.toLowerCase()}...`;
    dom.editorRoot.innerHTML = "";

    try {
        const item = await fetchJSON(`${meta.endpoint}/${id}`);
        renderEditor(item);
        dom.editorHeading.textContent = item.title ? item.title : `Edit ${meta.singular}`;
    } catch (error) {
        dom.editorRoot.innerHTML = `<p class="panel-empty">${escapeHtml(error.message || "Failed to load entry.")}</p>`;
    }
}

function startNewEntry(options = {}) {
    if (!options.skipConfirm && !confirmDiscardIfNeeded()) {
        return;
    }

    destroyEditor();
    state.selectedId = null;
    state.mode = "new";
    renderList();

    const item = defaultItemForSection(state.section);
    renderEditor(item);
    dom.editorHeading.textContent = sectionMeta[state.section].newLabel;
}

function renderEditor(item) {
    state.tags = Array.isArray(item.tags) ? [...item.tags] : [];
    state.slugTouched = state.mode === "edit";

    if (state.section === "blogs") {
        dom.editorRoot.innerHTML = renderBlogEditor(item);
        bindBlogEditor(item);
        updatePublicLink(item);
        return;
    }

    if (state.section === "projects") {
        dom.editorRoot.innerHTML = renderProjectEditor(item);
        bindProjectEditor(item);
        updatePublicLink(null);
        return;
    }

    dom.editorRoot.innerHTML = renderPieceEditor(item);
    bindPieceEditor(item);
    updatePublicLink(null);
}

function renderBlogEditor(item) {
    const currentStatus = normalizeBlogStatus(item.status);
    return `
        <form id="entity-form" class="editor-shell">
            <div class="form-grid">
                <div class="field">
                    <label for="title">Title</label>
                    <input id="title" name="title" value="${escapeAttr(item.title || "")}" required>
                </div>
                <div class="field">
                    <label for="slug">Slug</label>
                    <input id="slug" name="slug" value="${escapeAttr(item.slug || "")}" required>
                </div>
                <div class="field">
                    <label for="category">Category</label>
                    <input id="category" name="category" value="${escapeAttr(item.category || "")}" required>
                </div>
                <div class="field">
                    <label for="status">Status</label>
                    <select id="status" name="status">
                        <option value="draft"${currentStatus === "draft" ? " selected" : ""}>Draft</option>
                        <option value="published"${currentStatus === "published" ? " selected" : ""}>Published</option>
                    </select>
                </div>
                <div class="field">
                    <label for="thumbnail">Thumbnail URL</label>
                    <input id="thumbnail" name="thumbnail" value="${escapeAttr(item.thumbnail || "")}" required>
                </div>
                <div class="field">
                    <label for="date_published">Date Published</label>
                    <input id="date_published" name="date_published" value="${escapeAttr(item.date_published || "")}" required>
                </div>
                <div class="field">
                    <label for="last_updated">Last Updated</label>
                    <input id="last_updated" name="last_updated" value="${escapeAttr(item.last_updated || "")}" required>
                </div>
                <div class="field field-span-2">
                    <label for="description">Description</label>
                    <textarea id="description" name="description" required>${escapeHtml(item.description || "")}</textarea>
                </div>
            </div>

            <div class="markdown-workbench">
                <div class="markdown-pane">
                    <div class="pane-head">
                        <h4>Markdown Draft</h4>
                        <span class="meta-note">Autosaves locally while you type.</span>
                    </div>
                    <textarea id="markdown-editor">${escapeHtml(item.markdown || "")}</textarea>
                </div>

                <div class="preview-pane">
                    <div class="pane-head">
                        <h4>Live Preview</h4>
                        <span id="preview-status" class="meta-note">Waiting for changes</span>
                    </div>
                    <div id="blog-preview" class="preview-content"></div>
                </div>
            </div>

            <div class="editor-actions">
                <div class="editor-actions-left">
                    <button type="submit" class="primary-button">Save Blog Post</button>
                    ${state.mode === "edit" ? '<button type="button" id="delete-entry" class="danger-button">Delete</button>' : ""}
                </div>
                <div class="editor-actions-right">
                    <span class="meta-note">Drafts stay hidden from the public site and sitemap.</span>
                </div>
            </div>
        </form>
    `;
}

function renderProjectEditor(item) {
    return `
        <form id="entity-form" class="editor-shell">
            <div class="form-grid">
                <div class="field">
                    <label for="title">Title</label>
                    <input id="title" name="title" value="${escapeAttr(item.title || "")}" required>
                </div>
                <div class="field">
                    <label for="thumbnail">Thumbnail URL</label>
                    <input id="thumbnail" name="thumbnail" value="${escapeAttr(item.thumbnail || "")}">
                </div>
                <div class="field">
                    <label for="date_start">Start Date</label>
                    <input id="date_start" name="date_start" value="${escapeAttr(item.date_start || "")}">
                </div>
                <div class="field">
                    <label for="date_end">End Date</label>
                    <input id="date_end" name="date_end" value="${escapeAttr(item.date_end || "")}">
                </div>
                <div class="field field-span-2">
                    <label>Tags</label>
                    <div class="chip-editor">
                        <div id="tag-list" class="chip-list"></div>
                        <input id="tag-input" class="tag-input" type="text" placeholder="Add a tag and press Enter">
                    </div>
                </div>
                <div class="field field-span-2">
                    <label for="description">Description</label>
                    <textarea id="description" name="description" required>${escapeHtml(item.description || "")}</textarea>
                </div>
                <div class="field">
                    <label for="github_link">GitHub Link</label>
                    <input id="github_link" name="github_link" value="${escapeAttr(item.github_link || "")}">
                </div>
                <div class="field">
                    <label for="website_link">Website Link</label>
                    <input id="website_link" name="website_link" value="${escapeAttr(item.website_link || "")}">
                </div>
            </div>

            <div class="editor-actions">
                <div class="editor-actions-left">
                    <button type="submit" class="primary-button">Save Project</button>
                    ${state.mode === "edit" ? '<button type="button" id="delete-entry" class="danger-button">Delete</button>' : ""}
                </div>
                <div class="editor-actions-right">
                    <span class="meta-note">Tags are stored as a string array for the public project cards.</span>
                </div>
            </div>
        </form>
    `;
}

function renderPieceEditor(item) {
    return `
        <form id="entity-form" class="editor-shell">
            <div class="form-grid">
                <div class="field">
                    <label for="title">Title</label>
                    <input id="title" name="title" value="${escapeAttr(item.title || "")}" required>
                </div>
                <div class="field">
                    <label for="video_url">Video URL</label>
                    <input id="video_url" name="video_url" value="${escapeAttr(item.video_url || "")}">
                </div>
                <div class="field field-span-2">
                    <label for="description">Description</label>
                    <textarea id="description" name="description" required>${escapeHtml(item.description || "")}</textarea>
                </div>
            </div>

            <div class="editor-actions">
                <div class="editor-actions-left">
                    <button type="submit" class="primary-button">Save Piece</button>
                    ${state.mode === "edit" ? '<button type="button" id="delete-entry" class="danger-button">Delete</button>' : ""}
                </div>
                <div class="editor-actions-right">
                    <span class="meta-note">Use this for recordings, demos, or other hosted pieces.</span>
                </div>
            </div>
        </form>
    `;
}

function bindBlogEditor(item) {
    const form = document.getElementById("entity-form");
    const titleInput = document.getElementById("title");
    const slugInput = document.getElementById("slug");
    const previewStatus = document.getElementById("preview-status");
    const markdownTextarea = document.getElementById("markdown-editor");
    const savedSnapshot = snapshotForState({
        title: item.title || "",
        slug: item.slug || "",
        category: item.category || "",
        status: normalizeBlogStatus(item.status),
        thumbnail: item.thumbnail || "",
        date_published: item.date_published || "",
        last_updated: item.last_updated || "",
        description: item.description || "",
        markdown: item.markdown || "",
    });

    state.editor = createMarkdownEditor(markdownTextarea);

    const handleChange = () => {
        persistBlogAutosave();
        updateDirtyState();
        scheduleBlogPreview(previewStatus);
        updatePublicLink(currentFormData());
    };

    titleInput.addEventListener("input", () => {
        if (!state.slugTouched) {
            slugInput.value = slugify(titleInput.value);
        }
        handleChange();
    });

    slugInput.addEventListener("input", () => {
        state.slugTouched = true;
        handleChange();
    });

    bindStandardFormListeners(form, handleChange);

    state.editor.codemirror.on("change", () => {
        handleChange();
    });

    form.addEventListener("submit", async (event) => {
        event.preventDefault();
        await saveCurrentEntry();
    });

    bindDeleteButton();

    const restored = restoreAutosave(blogAutosaveKey(item));
    if (restored) {
        hydrateBlogForm(restored);
    }

    state.lastSavedSnapshot = savedSnapshot;
    setDirty(snapshotForState(currentFormData()) !== savedSnapshot);
    refreshBlogPreview(previewStatus);
}

function bindProjectEditor(item) {
    const form = document.getElementById("entity-form");
    renderTags();
    bindStandardFormListeners(form, updateDirtyState);

    const tagInput = document.getElementById("tag-input");
    tagInput.addEventListener("keydown", (event) => {
        if (event.key !== "Enter" && event.key !== ",") {
            return;
        }
        event.preventDefault();
        const value = tagInput.value.trim();
        if (!value) {
            return;
        }
        state.tags.push(value);
        state.tags = normalizeTags(state.tags);
        tagInput.value = "";
        renderTags();
        updateDirtyState();
    });

    form.addEventListener("submit", async (event) => {
        event.preventDefault();
        await saveCurrentEntry();
    });

    bindDeleteButton();

    state.lastSavedSnapshot = snapshotForState(currentFormData());
    setDirty(false);
}

function bindPieceEditor() {
    const form = document.getElementById("entity-form");
    bindStandardFormListeners(form, updateDirtyState);
    form.addEventListener("submit", async (event) => {
        event.preventDefault();
        await saveCurrentEntry();
    });
    bindDeleteButton();
    state.lastSavedSnapshot = snapshotForState(currentFormData());
    setDirty(false);
}

function bindStandardFormListeners(form, onChange) {
    Array.from(form.querySelectorAll("input, textarea, select")).forEach((element) => {
        element.addEventListener("input", onChange);
        element.addEventListener("change", onChange);
    });
}

function bindDeleteButton() {
    const deleteButton = document.getElementById("delete-entry");
    if (!deleteButton) {
        return;
    }
    deleteButton.addEventListener("click", async () => {
        await deleteCurrentEntry();
    });
}

function createMarkdownEditor(textarea) {
    if (window.EasyMDE) {
        return new EasyMDE({
            element: textarea,
            spellChecker: false,
            status: false,
            autoDownloadFontAwesome: false,
            placeholder: "Write your post in Markdown...",
            renderingConfig: {
                singleLineBreaks: false,
                codeSyntaxHighlighting: false,
            },
        });
    }

    return {
        value(newValue) {
            if (typeof newValue === "string") {
                textarea.value = newValue;
            }
            return textarea.value;
        },
        codemirror: {
            on() {},
        },
        toTextArea() {},
    };
}

async function saveCurrentEntry() {
    const payload = currentFormData();
    const meta = sectionMeta[state.section];
    const method = state.mode === "edit" && state.selectedId ? "PUT" : "POST";
    const url = method === "POST" ? meta.endpoint : `${meta.endpoint}/${state.selectedId}`;

    try {
        const response = await fetchJSON(url, {
            method,
            body: JSON.stringify(payload),
        });

        clearAutosaveForCurrentBlog();
        setDirty(false);

        if (state.section === "blogs" && response && response.document && response.document.id) {
            await loadCurrentSection(response.document.id);
            return;
        }

        if (state.section === "projects" && response && response.project && getItemId(response.project)) {
            await loadCurrentSection(getItemId(response.project));
            return;
        }

        if (state.section === "pieces" && response && response.piece && getItemId(response.piece)) {
            await loadCurrentSection(getItemId(response.piece));
            return;
        }

        if (method === "POST") {
            await loadCurrentSection();
            return;
        }

        await loadCurrentSection(state.selectedId);
    } catch (error) {
        window.alert(error.message || "Failed to save entry.");
    }
}

async function deleteCurrentEntry() {
    if (!state.selectedId) {
        return;
    }
    if (!window.confirm("Delete this entry? This cannot be undone.")) {
        return;
    }

    const meta = sectionMeta[state.section];
    try {
        await fetchJSON(`${meta.endpoint}/${state.selectedId}`, { method: "DELETE" });
        clearAutosaveForCurrentBlog();
        await loadCurrentSection();
    } catch (error) {
        window.alert(error.message || "Failed to delete entry.");
    }
}

function currentFormData() {
    if (state.section === "blogs") {
        return {
            title: valueOf("title"),
            slug: valueOf("slug"),
            category: valueOf("category"),
            status: normalizeBlogStatus(valueOf("status") || "draft"),
            thumbnail: valueOf("thumbnail"),
            date_published: valueOf("date_published"),
            last_updated: valueOf("last_updated"),
            description: valueOf("description"),
            markdown: state.editor ? state.editor.value() : valueOf("markdown-editor"),
        };
    }

    if (state.section === "projects") {
        return {
            title: valueOf("title"),
            thumbnail: valueOf("thumbnail"),
            date_start: valueOf("date_start"),
            date_end: valueOf("date_end"),
            tags: normalizeTags(state.tags),
            description: valueOf("description"),
            github_link: valueOf("github_link"),
            website_link: valueOf("website_link"),
        };
    }

    return {
        title: valueOf("title"),
        video_url: valueOf("video_url"),
        description: valueOf("description"),
    };
}

function updateDirtyState() {
    const dirty = snapshotForState(currentFormData()) !== state.lastSavedSnapshot;
    setDirty(dirty);
}

function setDirty(isDirty) {
    state.dirty = isDirty;
    dom.saveStatus.textContent = isDirty ? "Unsaved changes" : "Saved";
    dom.saveStatus.classList.toggle("is-dirty", isDirty);
}

function confirmDiscardIfNeeded() {
    if (!state.dirty) {
        return true;
    }
    return window.confirm("You have unsaved changes. Discard them?");
}

function valueOf(id) {
    const element = document.getElementById(id);
    return element ? element.value.trim() : "";
}

function defaultItemForSection(section) {
    if (section === "blogs") {
        const today = humanDate();
        return {
            title: "",
            slug: "",
            category: "",
            status: "draft",
            thumbnail: "",
            date_published: today,
            last_updated: today,
            description: "",
            markdown: "",
        };
    }

    if (section === "projects") {
        return {
            title: "",
            thumbnail: "",
            date_start: "",
            date_end: "",
            tags: [],
            description: "",
            github_link: "",
            website_link: "",
        };
    }

    return {
        title: "",
        video_url: "",
        description: "",
    };
}

function normalizeBlogStatus(status) {
    return String(status || "").trim().toLowerCase() === "draft" ? "draft" : "published";
}

function normalizeTags(tags) {
    return [...new Set((Array.isArray(tags) ? tags : []).map((tag) => String(tag).trim()).filter(Boolean))];
}

function renderTags() {
    const tagList = document.getElementById("tag-list");
    if (!tagList) {
        return;
    }
    tagList.innerHTML = "";
    state.tags.forEach((tag, index) => {
        const chip = document.createElement("div");
        chip.className = "chip";
        chip.innerHTML = `<span>${escapeHtml(tag)}</span><button type="button" aria-label="Remove ${escapeAttr(tag)}">&times;</button>`;
        chip.querySelector("button").addEventListener("click", () => {
            state.tags.splice(index, 1);
            renderTags();
            updateDirtyState();
        });
        tagList.appendChild(chip);
    });
}

function updatePublicLink(item) {
    if (state.section !== "blogs" || !item || normalizeBlogStatus(item.status) !== "published" || !item.slug) {
        dom.publicLink.classList.add("hidden");
        dom.publicLink.setAttribute("href", "/");
        return;
    }
    dom.publicLink.classList.remove("hidden");
    dom.publicLink.setAttribute("href", `/blog/${encodeURIComponent(item.slug)}`);
}

function blogAutosaveKey(item) {
    const id = getItemId(item);
    return `admin:blog-autosave:${id || "new"}`;
}

function persistBlogAutosave() {
    if (state.section !== "blogs") {
        return;
    }
    try {
        window.localStorage.setItem(blogAutosaveKey({ id: state.selectedId }), JSON.stringify(currentFormData()));
    } catch (error) {
        // ignore storage errors
    }
}

function restoreAutosave(key) {
    try {
        const raw = window.localStorage.getItem(key);
        return raw ? JSON.parse(raw) : null;
    } catch (error) {
        return null;
    }
}

function clearAutosaveForCurrentBlog() {
    if (state.section !== "blogs") {
        return;
    }
    try {
        window.localStorage.removeItem(blogAutosaveKey({ id: state.selectedId }));
        window.localStorage.removeItem("admin:blog-autosave:new");
    } catch (error) {
        // ignore storage errors
    }
}

function hydrateBlogForm(data) {
    const mappings = [
        "title",
        "slug",
        "category",
        "status",
        "thumbnail",
        "date_published",
        "last_updated",
        "description",
    ];
    mappings.forEach((key) => {
        const element = document.getElementById(key);
        if (element && typeof data[key] === "string") {
            element.value = data[key];
        }
    });
    if (state.editor && typeof data.markdown === "string") {
        state.editor.value(data.markdown);
    }
}

async function refreshBlogPreview(previewStatus) {
    const preview = document.getElementById("blog-preview");
    if (!preview) {
        return;
    }

    const requestId = ++state.previewRequestId;
    previewStatus.textContent = "Rendering preview...";
    preview.innerHTML = '<p class="preview-loading">Rendering preview...</p>';

    const data = currentFormData();
    try {
        const response = await fetchJSON("/api/admin/markdown/render", {
            method: "POST",
            body: JSON.stringify({ markdown: data.markdown }),
        });

        if (requestId !== state.previewRequestId) {
            return;
        }

        preview.innerHTML = `
            <div class="preview-header">
                <p class="eyebrow">${escapeHtml(data.category || "Draft preview")}</p>
                <h1>${escapeHtml(data.title || "Untitled post")}</h1>
                <p>${escapeHtml(data.description || "Add a description to see how the article intro will read.")}</p>
                <p>${escapeHtml([data.date_published, data.last_updated ? `Updated ${data.last_updated}` : ""].filter(Boolean).join(" • "))}</p>
            </div>
            <div>${response.html || ""}</div>
        `;
        previewStatus.textContent = "Preview synced";
    } catch (error) {
        if (requestId !== state.previewRequestId) {
            return;
        }
        preview.innerHTML = `<p class="panel-empty">${escapeHtml(error.message || "Preview failed.")}</p>`;
        previewStatus.textContent = "Preview unavailable";
    }
}

function scheduleBlogPreview(previewStatus) {
    window.clearTimeout(state.previewTimer);
    state.previewTimer = window.setTimeout(() => {
        refreshBlogPreview(previewStatus);
    }, 220);
}

function destroyEditor() {
    window.clearTimeout(state.previewTimer);
    if (state.editor && typeof state.editor.toTextArea === "function") {
        state.editor.toTextArea();
    }
    state.editor = null;
}

function getItemId(item) {
    if (!item || item.id == null) {
        return null;
    }
    if (typeof item.id === "string") {
        return item.id;
    }
    if (typeof item.id === "object" && typeof item.id.$oid === "string") {
        return item.id.$oid;
    }
    return String(item.id);
}

async function fetchJSON(url, options = {}) {
    const response = await fetch(url, {
        credentials: "same-origin",
        headers: {
            "Content-Type": "application/json",
            ...(options.headers || {}),
        },
        ...options,
    });

    const text = await response.text();
    let payload = null;
    try {
        payload = text ? JSON.parse(text) : null;
    } catch (error) {
        payload = null;
    }

    if (!response.ok) {
        const message = payload && payload.error ? payload.error : text || `Request failed with ${response.status}`;
        throw new Error(message);
    }

    return payload;
}

function snapshotForState(data) {
    return JSON.stringify(data || {});
}

function escapeHtml(value) {
    return String(value || "")
        .replaceAll("&", "&amp;")
        .replaceAll("<", "&lt;")
        .replaceAll(">", "&gt;")
        .replaceAll('"', "&quot;")
        .replaceAll("'", "&#39;");
}

function escapeAttr(value) {
    return escapeHtml(value);
}

function slugify(value) {
    return String(value || "")
        .trim()
        .toLowerCase()
        .replace(/[^a-z0-9]+/g, "-")
        .replace(/^-+|-+$/g, "");
}

function humanDate() {
    return new Date().toLocaleDateString("en-US", {
        month: "long",
        day: "numeric",
        year: "numeric",
    });
}

function showLoginError(message) {
    dom.loginError.textContent = message;
    dom.loginError.classList.remove("hidden");
}

function hideLoginError() {
    dom.loginError.textContent = "";
    dom.loginError.classList.add("hidden");
}
