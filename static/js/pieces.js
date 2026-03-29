const piecesState = {
    pieces: [],
    selectedIndex: 0,
};

const piecesDom = {
    app: document.getElementById("pieces-app"),
    shellWrap: document.querySelector(".pieces-shell-wrap"),
    summary: document.getElementById("pieces-summary"),
    count: document.getElementById("pieces-count"),
    stage: document.getElementById("piece-stage"),
    list: document.getElementById("pieces-list"),
};

function escapeHTML(value) {
    return String(value ?? "")
        .replaceAll("&", "&amp;")
        .replaceAll("<", "&lt;")
        .replaceAll(">", "&gt;")
        .replaceAll('"', "&quot;")
        .replaceAll("'", "&#39;");
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

function normalizeID(value, fallbackIndex) {
    if (typeof value === "string" && value.trim()) return value.trim();
    if (value && typeof value === "object") {
        if (typeof value.$oid === "string" && value.$oid.trim()) return value.$oid.trim();
        if (typeof value.hex === "string" && value.hex.trim()) return value.hex.trim();
    }
    return `piece-${fallbackIndex + 1}`;
}

function normalizeURL(value) {
    if (typeof value !== "string") return "";
    const trimmed = value.trim();
    if (!trimmed) return "";
    if (trimmed.startsWith("//")) return `https:${trimmed}`;
    if (trimmed.startsWith("http://") || trimmed.startsWith("https://") || trimmed.startsWith("/")) return trimmed;
    return `https://${trimmed}`;
}

function parseURL(value) {
    const normalized = normalizeURL(value);
    if (!normalized) return null;

    try {
        return new URL(normalized, window.location.origin);
    } catch (error) {
        return null;
    }
}

function youtubeIDFromURL(value) {
    const parsed = parseURL(value);
    if (!parsed) return "";

    const hostname = parsed.hostname.replace(/^www\./, "");
    const segments = parsed.pathname.split("/").filter(Boolean);

    if (hostname === "youtu.be" && segments[0]) return segments[0];
    if (hostname.endsWith("youtube.com") || hostname === "youtube-nocookie.com") {
        if (parsed.searchParams.get("v")) return parsed.searchParams.get("v");
        if (segments[0] === "embed" && segments[1]) return segments[1];
        if (segments[0] === "shorts" && segments[1]) return segments[1];
        if (segments[0] === "watch" && parsed.searchParams.get("v")) return parsed.searchParams.get("v");
    }

    return "";
}

function vimeoIDFromURL(value) {
    const parsed = parseURL(value);
    if (!parsed) return "";

    const hostname = parsed.hostname.replace(/^www\./, "");
    if (!hostname.endsWith("vimeo.com")) return "";

    const segments = parsed.pathname.split("/").filter(Boolean);
    const candidate = segments.find((segment) => /^\d+$/.test(segment));
    return candidate || "";
}

function resolvePieceMedia(value) {
    const normalized = normalizeURL(value);
    if (!normalized) {
        return { kind: "none", provider: "Notes only", url: "" };
    }

    const youtubeID = youtubeIDFromURL(normalized);
    if (youtubeID) {
        return {
            kind: "iframe",
            provider: "YouTube",
            url: normalized,
            embedURL: `https://www.youtube.com/embed/${encodeURIComponent(youtubeID)}`,
        };
    }

    const vimeoID = vimeoIDFromURL(normalized);
    if (vimeoID) {
        return {
            kind: "iframe",
            provider: "Vimeo",
            url: normalized,
            embedURL: `https://player.vimeo.com/video/${encodeURIComponent(vimeoID)}`,
        };
    }

    if (/\.(mp4|webm|ogg|mov)(?:$|\?)/i.test(normalized)) {
        return {
            kind: "video",
            provider: "Hosted video",
            url: normalized,
            src: normalized,
        };
    }

    return {
        kind: "link",
        provider: "External recording",
        url: normalized,
    };
}

function normalizePiece(piece, index) {
    const videoURL = firstNonEmptyText(piece?.video_url, piece?.videoURL, piece?.video, piece?.url, piece?.link);
    return {
        id: normalizeID(piece?.id ?? piece?._id, index),
        title: firstNonEmptyText(piece?.title, piece?.name) || `Untitled Piece ${index + 1}`,
        description: firstNonEmptyText(piece?.description, piece?.summary, piece?.notes) || "No notes have been added for this piece yet.",
        videoURL,
        media: resolvePieceMedia(videoURL),
    };
}

function normalizePieces(payload) {
    if (!Array.isArray(payload)) return [];
    return payload.map(normalizePiece);
}

function renderPieceMedia(piece) {
    if (piece.media.kind === "iframe") {
        return `
            <div class="piece-media-frame">
                <iframe
                    src="${escapeHTML(piece.media.embedURL)}"
                    title="${escapeHTML(piece.title)}"
                    loading="lazy"
                    allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture"
                    allowfullscreen
                ></iframe>
            </div>
        `;
    }

    if (piece.media.kind === "video") {
        return `
            <div class="piece-media-frame">
                <video controls preload="metadata" playsinline src="${escapeHTML(piece.media.src)}"></video>
            </div>
        `;
    }

    if (piece.media.kind === "link") {
        return `
            <div class="piece-media-placeholder">
                <span>Playback is hosted externally for this piece.</span>
                <a class="piece-primary-link" href="${escapeHTML(piece.media.url)}" target="_blank" rel="noreferrer">Open recording</a>
            </div>
        `;
    }

    return `
        <div class="piece-media-placeholder">
            <span>This entry is currently presented as notes only.</span>
        </div>
    `;
}

function renderStage() {
    const piece = piecesState.pieces[piecesState.selectedIndex];
    if (!piece || !piecesDom.stage) return;

    const pieceNumber = String(piecesState.selectedIndex + 1).padStart(2, "0");
    const actionLink = piece.videoURL
        ? `<a class="piece-secondary-link" href="${escapeHTML(piece.videoURL)}" target="_blank" rel="noreferrer">Open source link</a>`
        : "";

    piecesDom.stage.className = "piece-stage";
    piecesDom.stage.innerHTML = `
        <div class="piece-stage-panel">
            <div class="piece-stage-meta">
                <p class="piece-stage-label">Selected piece</p>
                <span class="piece-stage-provider">${escapeHTML(piece.media.provider)}</span>
            </div>
            <div class="piece-stage-header">
                <div>
                    <span class="piece-stage-index">${pieceNumber}</span>
                    <h2>${escapeHTML(piece.title)}</h2>
                </div>
                ${actionLink}
            </div>
            <p class="piece-stage-description">${escapeHTML(piece.description)}</p>
            <div class="piece-stage-media">
                ${renderPieceMedia(piece)}
            </div>
        </div>
    `;
}

function renderList() {
    if (!piecesDom.list) return;

    piecesDom.list.innerHTML = piecesState.pieces.map((piece, index) => `
        <button
            type="button"
            class="piece-list-item${index === piecesState.selectedIndex ? " active" : ""}"
            data-index="${index}"
            aria-pressed="${index === piecesState.selectedIndex ? "true" : "false"}"
        >
            <span class="piece-list-index">${String(index + 1).padStart(2, "0")}</span>
            <span class="piece-list-copy">
                <strong>${escapeHTML(piece.title)}</strong>
                <span>${escapeHTML(piece.description)}</span>
            </span>
            <span class="piece-list-provider">${escapeHTML(piece.media.provider)}</span>
        </button>
    `).join("");
}

function syncSummary() {
    if (piecesDom.count) {
        const total = piecesState.pieces.length;
        piecesDom.count.textContent = `${total} ${total === 1 ? "work" : "works"}`;
    }

    if (piecesDom.summary) {
        if (piecesState.pieces.length === 0) {
            piecesDom.summary.textContent = "No pieces have been published yet. New recordings and sketches will appear here once they're added.";
            return;
        }

        const latest = piecesState.pieces[0];
        piecesDom.summary.textContent = `${piecesState.pieces.length} ${piecesState.pieces.length === 1 ? "piece is" : "pieces are"} in the archive. The listening room opens on ${latest.title}.`;
    }
}

function syncQueryParam() {
    const piece = piecesState.pieces[piecesState.selectedIndex];
    if (!piece) return;

    const url = new URL(window.location.href);
    url.searchParams.set("piece", piece.id);
    window.history.replaceState({}, "", url);
}

function setSelectedIndex(index) {
    if (index < 0 || index >= piecesState.pieces.length) return;
    piecesState.selectedIndex = index;
    renderStage();
    renderList();
    syncQueryParam();
}

function readInitialIndex() {
    const url = new URL(window.location.href);
    const requestedID = trimmedText(url.searchParams.get("piece"));
    if (!requestedID) return 0;

    const matchedIndex = piecesState.pieces.findIndex((piece) => piece.id === requestedID);
    return matchedIndex >= 0 ? matchedIndex : 0;
}

function renderEmptyState(message) {
    syncSummary();
    if (piecesDom.shellWrap) {
        piecesDom.shellWrap.style.display = "none";
    }
    if (piecesDom.stage) {
        piecesDom.stage.className = "piece-stage piece-stage-empty";
        piecesDom.stage.innerHTML = `
            <div class="piece-stage-panel">
                <p class="piece-stage-label">Selected piece</p>
                <h2>No pieces yet</h2>
                <p class="piece-stage-description">${escapeHTML(message)}</p>
                <div class="piece-stage-media">
                    <div class="piece-media-placeholder">
                        <span>The archive is ready whenever the first recording is published.</span>
                    </div>
                </div>
            </div>
        `;
    }

    if (piecesDom.list) {
        piecesDom.list.innerHTML = `<p class="pieces-rail-empty">${escapeHTML(message)}</p>`;
    }
}

async function loadPieces() {
    if (!piecesDom.app) return;

    try {
        const response = await fetch("/api/pieces");
        if (!response.ok) throw new Error("Failed to load pieces");

        const payload = await response.json().catch(() => []);
        piecesState.pieces = normalizePieces(payload);

        if (piecesState.pieces.length === 0) {
            renderEmptyState("No published pieces were found.");
            return;
        }

        piecesState.selectedIndex = readInitialIndex();
        syncSummary();
        renderStage();
        renderList();
        syncQueryParam();

        piecesDom.list.addEventListener("click", (event) => {
            const button = event.target.closest(".piece-list-item");
            if (!button) return;
            setSelectedIndex(Number(button.dataset.index));
        });
    } catch (error) {
        renderEmptyState("The pieces archive could not be loaded right now.");
    }
}

loadPieces();
