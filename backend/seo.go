package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type sitemapPage struct {
	Path string
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

type sitemapURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	XMLNS   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

// Add public static pages here as the site grows.
var sitemapStaticPages = []sitemapPage{
	{Path: "/"},
	{Path: "/blog"},
	{Path: "/projects"},
}

func robotsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("User-agent: *\nAllow: /\n\nSitemap: " + absoluteURL(r, "/sitemap.xml") + "\n"))
}

func sitemapHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	urls := make([]sitemapURL, 0, len(sitemapStaticPages)+8)
	for _, page := range sitemapStaticPages {
		urls = append(urls, sitemapURL{Loc: absoluteURL(r, page.Path)})
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	cur, err := blogCollection.Find(ctx, publishedBlogFilter())
	if err != nil {
		http.Error(w, "failed to build sitemap", http.StatusInternalServerError)
		log.Printf("sitemapHandler: find error: %v", err)
		return
	}
	defer cur.Close(ctx)

	var posts []BlogPost
	if err := cur.All(ctx, &posts); err != nil {
		http.Error(w, "failed to build sitemap", http.StatusInternalServerError)
		log.Printf("sitemapHandler: decode error: %v", err)
		return
	}

	for i := range posts {
		if err := ensureBlogSlug(ctx, &posts[i]); err != nil {
			http.Error(w, "failed to build sitemap", http.StatusInternalServerError)
			log.Printf("sitemapHandler: ensure slug error: %v", err)
			return
		}

		entry := sitemapURL{
			Loc: absoluteURL(r, "/blog/"+url.PathEscape(posts[i].Slug)),
		}
		if updatedAt, ok := parseSEODate(posts[i].LastUpdated); ok {
			entry.LastMod = updatedAt.Format("2006-01-02")
		} else if publishedAt, ok := parseSEODate(posts[i].DatePublished); ok {
			entry.LastMod = publishedAt.Format("2006-01-02")
		}
		urls = append(urls, entry)
	}

	payload := sitemapURLSet{
		XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}

	output, err := xml.MarshalIndent(payload, "", "  ")
	if err != nil {
		http.Error(w, "failed to build sitemap", http.StatusInternalServerError)
		log.Printf("sitemapHandler: marshal error: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write([]byte(xml.Header))
	_, _ = w.Write(output)
}

func absoluteURL(r *http.Request, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}

	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	if r == nil || r.Host == "" {
		return path
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded != "" {
		scheme = forwarded
	}

	return (&url.URL{
		Scheme: scheme,
		Host:   r.Host,
		Path:   path,
	}).String()
}

func absoluteAssetURL(r *http.Request, assetPath, fallback string) string {
	assetPath = strings.TrimSpace(assetPath)
	if assetPath == "" {
		assetPath = fallback
	}
	return absoluteURL(r, assetPath)
}

func parseSEODate(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02",
		"January 2 2006",
		"January 2, 2006",
		"Jan 2 2006",
		"Jan 2, 2006",
	}

	for _, layout := range layouts {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, true
		}
	}

	return time.Time{}, false
}

func articleJSONLD(r *http.Request, post BlogPost) template.JS {
	postURL := absoluteURL(r, "/blog/"+url.PathEscape(post.Slug))

	payload := map[string]interface{}{
		"@context":         "https://schema.org",
		"@type":            "BlogPosting",
		"headline":         post.Title,
		"description":      post.Description,
		"url":              postURL,
		"mainEntityOfPage": postURL,
		"author": map[string]string{
			"@type": "Person",
			"name":  "Vihaan Pundir",
		},
		"publisher": map[string]string{
			"@type": "Person",
			"name":  "Vihaan Pundir",
		},
		"image": absoluteAssetURL(r, post.Thumbnail, "/img/post.avif"),
	}

	if post.Category != "" {
		payload["articleSection"] = post.Category
	}
	if publishedAt, ok := parseSEODate(post.DatePublished); ok {
		payload["datePublished"] = publishedAt.Format(time.RFC3339)
	}
	if updatedAt, ok := parseSEODate(post.LastUpdated); ok {
		payload["dateModified"] = updatedAt.Format(time.RFC3339)
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		log.Printf("articleJSONLD: marshal error: %v", err)
		return template.JS("{}")
	}

	return template.JS(raw)
}
