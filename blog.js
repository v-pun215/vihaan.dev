async function loadingBuffer() {
    loading_span = document.getElementById("loading-buffer");
    try {
        const response = await fetch('http://localhost:8080/api/blogposts');
        if (!response.ok) throw Error('no api');
        const list = await response.json().catch(() => [])
        if (!Array.isArray(list) || list.length === 0 ) {
            loading_span.innerHTML = "No blog posts found."
        }
        for (const item of list) {
            renderPost(item);
        }
        loading_span.style.display = "none";

    } catch (error) {
        alert(error);
         const sample =[{"title":"Sample Blog Post","category":"education","date_published":"October 28 2025","last_updated":"October 28 2025"},{"title":"Using Markdown in Blog","category":"Development","date_published":"2025-10-28","last_updated":"2025-10-28"}];
         for (const item of sample) {
            renderPost(item);
         }
         loading_span.style.display = "none";
    }
}

function renderPost(data) {
    const blogpost_div = document.getElementById("blogpost-div");
    if (!blogpost_div) return;
    const post = `
        <div class="blogpost-short">
            <p class="title">${data.title}</p>
            <p class="metadata">${data.category}, ${data.date_published}</p>
        </div>
    `;
    blogpost_div.insertAdjacentHTML('beforeend', post);
}