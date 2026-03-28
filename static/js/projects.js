async function loadProjects() {
    const projectsList = document.getElementById("projects-list");
    const loading = document.getElementById("projects-loading");
    if (!projectsList || !loading) return;

    try {
        const response = await fetch("/api/projects");
        if (!response.ok) throw new Error("no api");

        const payload = await response.json().catch(() => null);
        const projects = Array.isArray(payload) ? payload : [];

        if (projects.length === 0) {
            loading.textContent = "No projects found.";
            return;
        }

        projectsList.innerHTML = "";
        for (const project of projects) {
            projectsList.insertAdjacentHTML("beforeend", renderProject(project));
        }
    } catch (error) {
        loading.textContent = "No projects found.";
    }
}

function escapeHTML(value) {
    return String(value ?? "")
        .replaceAll("&", "&amp;")
        .replaceAll("<", "&lt;")
        .replaceAll(">", "&gt;")
        .replaceAll('"', "&quot;")
        .replaceAll("'", "&#39;");
}

function normalizeExternalURL(value) {
    if (typeof value !== "string") return "";
    const trimmed = value.trim();
    if (!trimmed) return "";
    if (trimmed.startsWith("http://") || trimmed.startsWith("https://")) return trimmed;
    return `https://${trimmed}`;
}

function trimmedText(value) {
    return typeof value === "string" ? value.trim() : "";
}

function firstNonEmptyText(...values) {
    for (const value of values) {
        const text = trimmedText(value);
        if (text) return text;
    }
    return "";
}

function renderProjectDate(project) {
    const start = firstNonEmptyText(project?.date_start, project?.start_date, project?.start);
    const end = firstNonEmptyText(project?.date_end, project?.end_date, project?.end);

    if (start && end) return `${start} - ${end}`;
    if (start) return start;
    if (end) return end;
    return "";
}

function renderProjectMedia(project) {
    const thumbnail = firstNonEmptyText(
        project?.thumbnail,
        project?.thumbnail_url,
        project?.image,
        project?.image_url,
    ) || "/img/post.avif";
    return `<img class="project-card-image" src="${escapeHTML(thumbnail)}" alt="${escapeHTML(project?.title || "Project thumbnail")}" onerror="this.onerror=null;this.src='/img/post.avif';">`;
}

function readProjectTags(project) {
    const candidates = [project?.tags, project?.project_tags];

    for (const candidate of candidates) {
        if (Array.isArray(candidate)) {
            return candidate.map((tag) => trimmedText(tag)).filter(Boolean);
        }
        if (typeof candidate === "string") {
            return candidate.split(",").map((tag) => trimmedText(tag)).filter(Boolean);
        }
    }

    return [];
}

function renderProject(project) {
    const title = escapeHTML(project?.title);
    const description = escapeHTML(project?.description);
    const dateRange = renderProjectDate(project);
    const websiteLink = normalizeExternalURL(project?.website_link);
    const githubLink = normalizeExternalURL(project?.github_link);
    const tags = readProjectTags(project);

    let links = "";
    if (websiteLink) {
        links += `<a class="project-link primary" href="${escapeHTML(websiteLink)}" target="_blank" rel="noreferrer">Visit Project</a>`;
    }
    if (githubLink) {
        links += `<a class="project-link" href="${escapeHTML(githubLink)}" target="_blank" rel="noreferrer">GitHub</a>`;
    }

    const tagsMarkup = tags.length
        ? `<div class="project-card-tags">${tags.map((tag) => `<span class="project-tag">${escapeHTML(tag)}</span>`).join("")}</div>`
        : "";

    return `
        <article class="project-card">
            <div class="project-card-visual">
                ${renderProjectMedia(project)}
            </div>
            <div class="project-card-body">
                <h2 class="project-card-title">${title}</h2>
                ${dateRange ? `<p class="project-card-date">${escapeHTML(dateRange)}</p>` : ""}
                <p class="project-card-description">${description}</p>
                ${tagsMarkup}
            </div>
            ${links ? `<div class="project-card-links">${links}</div>` : ""}
        </article>
    `;
}

loadProjects();
