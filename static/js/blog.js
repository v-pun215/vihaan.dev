async function loadingBuffer() {
    const loading_span = document.getElementById("loading-buffer");
    if (!loading_span) return;

    try {
        const response = await fetch('/api/blogposts');
        if (!response.ok) throw Error('no api');
        const payload = await response.json().catch(() => null);
        const list = Array.isArray(payload) ? payload : [];

        if (list.length === 0) {
            loading_span.innerHTML = "No blog posts found.";
            return;
        }

        for (const item of list) {
            renderPost(item);
        }
        loading_span.style.display = "none";

    } catch (error) {
        loading_span.innerHTML = "No blog posts found.";
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

function renderPost(data) {
    const blogpost_div = document.getElementById("blogpost-div");
    if (!blogpost_div) return;
    const slug = typeof data?.slug === "string" ? data.slug.trim() : "";
    const href = slug ? `/blog/${encodeURIComponent(slug)}` : "/blog";
    const metadata = [data?.category, data?.date_published]
        .map((value) => typeof value === "string" ? value.trim() : "")
        .filter(Boolean)
        .join(", ");
    const post = `
        <a class="blogpost-short" href="${href}">
            <p class="title">${escapeHTML(data?.title)}</p>
            <p class="metadata">${escapeHTML(metadata)}</p>
        </a>
    `;
    blogpost_div.insertAdjacentHTML('beforeend', post);
}
