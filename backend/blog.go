package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	errMissingSlug   = errors.New("slug is required")
	errDuplicateSlug = errors.New("slug already exists")
	errInvalidStatus = errors.New("invalid blog status")
)

const (
	blogStatusDraft     = "draft"
	blogStatusPublished = "published"
	maxBlogSearchQuery  = 200
)

type viewPost struct {
	Title         string
	Description   string
	Category      string
	DatePublished string
	Thumbnail     string
	Link          string
	ReadingTime   int
}

type tocEntry struct {
	Level int
	Text  string
	ID    string
}

type blogListPageData struct {
	Posts           []viewPost
	PageDescription string
	CanonicalURL    string
	OGImageURL      string
}

type blogPageData struct {
	Title            string
	Description      string
	Category         string
	DatePublished    string
	LastUpdated      string
	Thumbnail        string
	CanonicalURL     string
	OGImageURL       string
	Slug             string
	JSONLD           template.JS
	Content          template.HTML
	ReadingTime      int
	WordCount        int
	WordCountFmt     string
	Headings         []tocEntry
	HasTOC           bool
	PrevPost         *viewPost
	NextPost         *viewPost
	Related          []viewPost
	DatePublishedISO string
	LastUpdatedISO   string
}

var blogMarkdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
)

var blogListTmpl = template.Must(template.ParseFiles("templates/blog_list.html"))
var blogPostTmpl = template.Must(template.ParseFiles("templates/blog_post.html"))

func blogListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	cur, err := blogCollection.Find(ctx, publishedBlogFilter(), options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}))
	if err != nil {
		http.Error(w, "failed to fetch posts", http.StatusInternalServerError)
		log.Printf("blogListHandler: find error: %v", err)
		return
	}
	defer cur.Close(ctx)

	var posts []BlogPost
	if err := cur.All(ctx, &posts); err != nil {
		http.Error(w, "failed to decode posts", http.StatusInternalServerError)
		log.Printf("blogListHandler: decode error: %v", err)
		return
	}

	view := make([]viewPost, 0, len(posts))
	for i := range posts {
		if err := ensureBlogSlug(ctx, &posts[i]); err != nil {
			http.Error(w, "failed to prepare posts", http.StatusInternalServerError)
			log.Printf("blogListHandler: ensure slug error: %v", err)
			return
		}
		normalizeBlogDisplayDates(&posts[i])
		view = append(view, viewPost{
			Title:         posts[i].Title,
			Description:   posts[i].Description,
			Category:      posts[i].Category,
			DatePublished: posts[i].DatePublished,
			Thumbnail:     posts[i].Thumbnail,
			Link:          "/blog/" + url.PathEscape(posts[i].Slug),
			ReadingTime:   readingTimeMinutes(countWords(posts[i].Markdown)),
		})
	}

	data := blogListPageData{
		Posts:           view,
		PageDescription: "Writing about projects, code, and what I'm learning.",
		CanonicalURL:    absoluteURL(r, "/blog"),
		OGImageURL:      absoluteAssetURL(r, "/img/banner.jpg", "/img/post.avif"),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := blogListTmpl.Execute(w, data); err != nil {
		log.Printf("blogListHandler: template execute error: %v", err)
	}
}

func blogPostPageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	slug := strings.TrimPrefix(r.URL.Path, "/blog/")
	if slug == "" || strings.Contains(slug, "/") {
		http.NotFound(w, r)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	post, err := findPublicBlogBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to fetch post", http.StatusInternalServerError)
		log.Printf("blogPostPageHandler: find error: %v", err)
		return
	}

	if err := ensureBlogSlug(ctx, &post); err != nil {
		http.Error(w, "failed to prepare post", http.StatusInternalServerError)
		log.Printf("blogPostPageHandler: ensure slug error: %v", err)
		return
	}

	if slug != post.Slug {
		http.Redirect(w, r, "/blog/"+url.PathEscape(post.Slug), http.StatusMovedPermanently)
		return
	}
	normalizeBlogDisplayDates(&post)

	content, err := renderMarkdown(post.Markdown)
	if err != nil {
		http.Error(w, "failed to render post", http.StatusInternalServerError)
		log.Printf("blogPostPageHandler: markdown error: %v", err)
		return
	}

	words := countWords(post.Markdown)
	readMins := readingTimeMinutes(words)
	headings, contentWithIDs := tocFromRenderedHTML(string(content))
	content = template.HTML(contentWithIDs)

	var datePubISO, lastUpdISO string
	if t, ok := parseSEODate(post.DatePublished); ok {
		datePubISO = t.Format("2006-01-02")
	}
	if t, ok := parseSEODate(post.LastUpdated); ok {
		lastUpdISO = t.Format("2006-01-02")
	}

	prevPost := findAdjacentPost(ctx, post, false)
	nextPost := findAdjacentPost(ctx, post, true)
	related := findRelatedPosts(ctx, post, 3)

	page := blogPageData{
		Title:            post.Title,
		Description:      post.Description,
		Category:         post.Category,
		DatePublished:    post.DatePublished,
		LastUpdated:      post.LastUpdated,
		Thumbnail:        post.Thumbnail,
		CanonicalURL:     absoluteURL(r, "/blog/"+url.PathEscape(post.Slug)),
		OGImageURL:       absoluteAssetURL(r, post.Thumbnail, "/img/post.avif"),
		Slug:             post.Slug,
		JSONLD:           articleJSONLD(r, post),
		Content:          content,
		ReadingTime:      readMins,
		WordCount:        words,
		WordCountFmt:     formatWordCount(words),
		Headings:         headings,
		HasTOC:           len(headings) >= 2,
		PrevPost:         prevPost,
		NextPost:         nextPost,
		Related:          related,
		DatePublishedISO: datePubISO,
		LastUpdatedISO:   lastUpdISO,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := blogPostTmpl.Execute(w, page); err != nil {
		log.Printf("blogPostPageHandler: template execute error: %v", err)
	}
}

func all_blogs_handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cursor, err := blogCollection.Find(ctx, publishedBlogFilter(), options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}))
	if err != nil {
		log.Printf("all_blogs_handler: find: %v", err)
		http.Error(w, "failed to fetch blog posts", http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var blogs []BlogPost
	if err := cursor.All(ctx, &blogs); err != nil {
		log.Printf("all_blogs_handler: decode: %v", err)
		http.Error(w, "failed to decode blog posts", http.StatusInternalServerError)
		return
	}
	if blogs == nil {
		blogs = []BlogPost{}
	}
	for i := range blogs {
		if err := ensureBlogSlug(ctx, &blogs[i]); err != nil {
			log.Printf("all_blogs_handler: slug: %v", err)
			http.Error(w, "failed to prepare blog posts", http.StatusInternalServerError)
			return
		}
		normalizeBlogDisplayDates(&blogs[i])
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(blogs)
}

func addBlogpost(ctx context.Context, blog BlogPost) (interface{}, bson.M, error) {
	if strings.TrimSpace(blog.Slug) == "" {
		return nil, nil, errMissingSlug
	}

	if blog.Title == "" ||
		blog.Thumbnail == "" ||
		blog.Category == "" ||
		blog.DatePublished == "" ||
		blog.LastUpdated == "" ||
		blog.Description == "" ||
		blog.Markdown == "" {
		return nil, nil, fmt.Errorf("missing required blog fields")
	}
	blog.Status = normalizeBlogStatus(blog.Status)
	if blog.Status == "" {
		return nil, nil, errInvalidStatus
	}

	slug := normalizeSlug(blog.Slug)
	if slug == "" {
		return nil, nil, errMissingSlug
	}
	exists, err := slugExists(ctx, slug, primitive.NilObjectID)
	if err != nil {
		return nil, nil, err
	}
	if exists {
		return nil, nil, errDuplicateSlug
	}
	blog.Slug = slug

	result, err := blogCollection.InsertOne(ctx, blog)
	if err != nil {
		return nil, nil, err
	}
	if oid, ok := result.InsertedID.(primitive.ObjectID); ok {
		blog.ID = oid
	}

	doc := bson.M{
		"id":             blog.ID.Hex(),
		"slug":           blog.Slug,
		"title":          blog.Title,
		"thumbnail":      blog.Thumbnail,
		"category":       blog.Category,
		"date_published": blog.DatePublished,
		"last_updated":   blog.LastUpdated,
		"description":    blog.Description,
		"markdown":       blog.Markdown,
		"status":         blog.Status,
	}

	return result.InsertedID, doc, nil
}

func postBlogHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed (only POST)", http.StatusMethodNotAllowed)
		return
	}

	var blog BlogPost
	if err := json.NewDecoder(r.Body).Decode(&blog); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	id, doc, err := addBlogpost(ctx, blog)
	if err != nil {
		switch {
		case errors.Is(err, errMissingSlug):
			http.Error(w, "failed to insert blog post: slug is required", http.StatusBadRequest)
		case errors.Is(err, errInvalidStatus):
			http.Error(w, "failed to insert blog post: invalid status", http.StatusBadRequest)
		case errors.Is(err, errDuplicateSlug):
			http.Error(w, "failed to insert blog post: slug already exists", http.StatusConflict)
		default:
			log.Printf("postBlogHandler: %v", err)
			http.Error(w, "failed to insert blog post", http.StatusInternalServerError)
		}
		return
	}

	resp := bson.M{
		"inserted_id": id,
		"document":    doc,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

func deleteAllBlogPosts(ctx context.Context) (int64, error) {
	coll := client.Database("main").Collection("blogposts")
	res, err := coll.DeleteMany(ctx, bson.M{})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

func deleteAllHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed (only POST)", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	count, err := deleteAllBlogPosts(ctx)
	if err != nil {
		log.Printf("deleteAllHandler: %v", err)
		http.Error(w, "failed to delete blog posts", http.StatusInternalServerError)
		return
	}

	resp := bson.M{
		"message":       "all blogposts deleted successfully",
		"deleted_count": count,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func updateBlogByID(ctx context.Context, idHex string, updates bson.M) (int64, error) {
	oid, err := primitive.ObjectIDFromHex(idHex)
	if err != nil {
		return 0, fmt.Errorf("invalid id: %w", err)
	}

	if len(updates) == 0 {
		return 0, fmt.Errorf("no updates provided")
	}

	delete(updates, "_id")
	delete(updates, "id")

	if rawSlug, ok := updates["slug"]; ok {
		slug, ok := rawSlug.(string)
		if !ok {
			return 0, fmt.Errorf("slug must be a string")
		}
		normalized := normalizeSlug(slug)
		if normalized == "" {
			return 0, fmt.Errorf("slug is required")
		}
		exists, err := slugExists(ctx, normalized, oid)
		if err != nil {
			return 0, err
		}
		if exists {
			return 0, errDuplicateSlug
		}
		updates["slug"] = normalized
	}
	if rawStatus, ok := updates["status"]; ok {
		status, ok := rawStatus.(string)
		if !ok {
			return 0, fmt.Errorf("status must be a string")
		}
		normalized := normalizeBlogStatus(status)
		if normalized == "" {
			return 0, errInvalidStatus
		}
		updates["status"] = normalized
	}

	updates["last_updated"] = currentBlogDisplayDate()

	res, err := blogCollection.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": updates})
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

func editBlogHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed (only POST)"}`, http.StatusMethodNotAllowed)
		return
	}

	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	rawID, ok := payload["id"]
	if !ok {
		http.Error(w, `{"error":"id is required"}`, http.StatusBadRequest)
		return
	}
	idStr, ok := rawID.(string)
	if !ok || idStr == "" {
		http.Error(w, `{"error":"id must be a hex string"}`, http.StatusBadRequest)
		return
	}

	allowed := map[string]bool{
		"slug":           true,
		"title":          true,
		"thumbnail":      true,
		"category":       true,
		"date_published": true,
		"last_updated":   true,
		"description":    true,
		"markdown":       true,
		"status":         true,
	}
	updates := bson.M{}
	for k, v := range payload {
		if k == "id" {
			continue
		}
		if !allowed[k] {
			continue
		}
		s, ok := v.(string)
		if !ok {
			http.Error(w, `{"error":"all updatable fields must be strings"}`, http.StatusBadRequest)
			return
		}
		if s != "" {
			updates[k] = s
		}
	}

	if len(updates) == 0 {
		http.Error(w, `{"error":"no valid fields provided to update; provide at least one"}`, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	modified, err := updateBlogByID(ctx, idStr, updates)
	if err != nil {
		switch {
		case errors.Is(err, errDuplicateSlug):
			http.Error(w, `{"error":"slug already exists"}`, http.StatusConflict)
		case errors.Is(err, errInvalidStatus):
			http.Error(w, `{"error":"invalid status"}`, http.StatusBadRequest)
		default:
			log.Printf("editBlogHandler: %v", err)
			http.Error(w, `{"error":"failed to update blog post"}`, http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(bson.M{
		"modified_count": modified,
	})
}

func getBlogHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed (only GET)"}`, http.StatusMethodNotAllowed)
		return
	}

	idStr := strings.TrimSpace(r.URL.Query().Get("id"))
	slug := strings.TrimSpace(r.URL.Query().Get("slug"))
	if idStr == "" && slug == "" {
		http.Error(w, `{"error":"id or slug query param required"}`, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var (
		blog BlogPost
		err  error
	)
	if slug != "" {
		blog, err = findPublicBlogBySlug(ctx, slug)
	} else {
		blog, err = findPublicBlogByID(ctx, idStr)
	}
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			http.Error(w, `{"error":"post not found"}`, http.StatusNotFound)
			return
		}
		log.Printf("getBlogHandler: %v", err)
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	if err := ensureBlogSlug(ctx, &blog); err != nil {
		log.Printf("getBlogHandler ensure slug: %v", err)
		http.Error(w, `{"error":"failed to load post"}`, http.StatusInternalServerError)
		return
	}
	normalizeBlogDisplayDates(&blog)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(blog)
}

func searchBlogHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed (only GET)"}`, http.StatusMethodNotAllowed)
		return
	}

	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		http.Error(w, `{"error":"q query param required"}`, http.StatusBadRequest)
		return
	}
	if len(q) > maxBlogSearchQuery {
		http.Error(w, `{"error":"q is too long"}`, http.StatusBadRequest)
		return
	}
	qSafe := regexp.QuoteMeta(q)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	filter := bson.M{
		"$and": []bson.M{
			publishedBlogFilter(),
			{
				"$or": []bson.M{
					{"title": bson.M{"$regex": qSafe, "$options": "i"}},
					{"description": bson.M{"$regex": qSafe, "$options": "i"}},
					{"category": bson.M{"$regex": qSafe, "$options": "i"}},
					{"slug": bson.M{"$regex": qSafe, "$options": "i"}},
				},
			},
		},
	}

	cursor, err := blogCollection.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}))
	if err != nil {
		log.Printf("searchBlogHandler: find: %v", err)
		http.Error(w, `{"error":"search failed"}`, http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var results []BlogPost
	if err := cursor.All(ctx, &results); err != nil {
		log.Printf("searchBlogHandler: decode: %v", err)
		http.Error(w, `{"error":"search failed"}`, http.StatusInternalServerError)
		return
	}
	for i := range results {
		if err := ensureBlogSlug(ctx, &results[i]); err != nil {
			log.Printf("searchBlogHandler: slug: %v", err)
			http.Error(w, `{"error":"search failed"}`, http.StatusInternalServerError)
			return
		}
		normalizeBlogDisplayDates(&results[i])
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(results)
}

func ensureBlogSlug(ctx context.Context, blog *BlogPost) error {
	if blog == nil {
		return fmt.Errorf("blog is nil")
	}
	if strings.TrimSpace(blog.Slug) != "" {
		normalized := normalizeSlug(blog.Slug)
		if normalized == "" {
			return fmt.Errorf("slug is required")
		}
		if normalized == blog.Slug {
			return nil
		}
		blog.Slug = normalized
		if blog.ID.IsZero() {
			return nil
		}
		_, err := blogCollection.UpdateOne(ctx, bson.M{"_id": blog.ID}, bson.M{"$set": bson.M{"slug": normalized}})
		return err
	}
	if strings.TrimSpace(blog.Title) == "" {
		return fmt.Errorf("blog title is required to create a legacy slug")
	}

	slug, err := uniqueSlug(ctx, blog.Title, blog.ID)
	if err != nil {
		return err
	}
	blog.Slug = slug
	if blog.ID.IsZero() {
		return nil
	}

	_, err = blogCollection.UpdateOne(ctx, bson.M{"_id": blog.ID}, bson.M{"$set": bson.M{"slug": slug}})
	return err
}

func findBlogByID(ctx context.Context, idHex string) (BlogPost, error) {
	oid, err := primitive.ObjectIDFromHex(idHex)
	if err != nil {
		return BlogPost{}, fmt.Errorf("invalid id: %w", err)
	}

	var blog BlogPost
	if err := blogCollection.FindOne(ctx, bson.M{"_id": oid}).Decode(&blog); err != nil {
		return BlogPost{}, err
	}
	return blog, nil
}

func findPublicBlogByID(ctx context.Context, idHex string) (BlogPost, error) {
	oid, err := primitive.ObjectIDFromHex(idHex)
	if err != nil {
		return BlogPost{}, fmt.Errorf("invalid id: %w", err)
	}

	var blog BlogPost
	filter := bson.M{
		"$and": []bson.M{
			{"_id": oid},
			publishedBlogFilter(),
		},
	}
	if err := blogCollection.FindOne(ctx, filter).Decode(&blog); err != nil {
		return BlogPost{}, err
	}
	return blog, nil
}

func findBlogBySlug(ctx context.Context, slug string) (BlogPost, error) {
	var blog BlogPost
	raw := strings.TrimSpace(slug)
	normalized := normalizeSlug(raw)
	filter := bson.M{"slug": raw}
	if normalized != "" && normalized != raw {
		filter = bson.M{
			"$or": []bson.M{
				{"slug": raw},
				{"slug": normalized},
			},
		}
	} else if normalized != "" {
		filter = bson.M{"slug": normalized}
	}
	if err := blogCollection.FindOne(ctx, filter).Decode(&blog); err != nil {
		return BlogPost{}, err
	}
	return blog, nil
}

func findPublicBlogBySlug(ctx context.Context, slug string) (BlogPost, error) {
	var blog BlogPost
	raw := strings.TrimSpace(slug)
	normalized := normalizeSlug(raw)

	slugClauses := []bson.M{{"slug": raw}}
	if normalized != "" && normalized != raw {
		slugClauses = append(slugClauses, bson.M{"slug": normalized})
	} else if normalized != "" {
		slugClauses = []bson.M{{"slug": normalized}}
	}

	filter := bson.M{
		"$and": []bson.M{
			publishedBlogFilter(),
			{"$or": slugClauses},
		},
	}
	if err := blogCollection.FindOne(ctx, filter).Decode(&blog); err != nil {
		return BlogPost{}, err
	}
	return blog, nil
}

func slugExists(ctx context.Context, slug string, excludeID primitive.ObjectID) (bool, error) {
	var blog BlogPost
	err := blogCollection.FindOne(ctx, bson.M{"slug": slug}).Decode(&blog)
	if err == mongo.ErrNoDocuments {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !excludeID.IsZero() && blog.ID == excludeID {
		return false, nil
	}
	return true, nil
}

func uniqueSlug(ctx context.Context, value string, excludeID primitive.ObjectID) (string, error) {
	base := normalizeSlug(value)
	if base == "" {
		base = "post"
	}
	candidate := base
	for i := 2; ; i++ {
		exists, err := slugExists(ctx, candidate, excludeID)
		if err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
}

func normalizeSlug(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var b strings.Builder
	lastDash := false

	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if b.Len() > 0 && !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}

	return strings.Trim(b.String(), "-")
}

func normalizeBlogStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", blogStatusPublished:
		return blogStatusPublished
	case blogStatusDraft:
		return blogStatusDraft
	default:
		return ""
	}
}

func publishedBlogFilter() bson.M {
	return bson.M{
		"$or": []bson.M{
			{"status": blogStatusPublished},
			{"status": ""},
			{"status": bson.M{"$exists": false}},
		},
	}
}

func currentBlogDisplayDate() string {
	return time.Now().Format("January 2 2006")
}

// canonicalBlogDisplayDate reformats legacy stored dates such as
// "May 20, 2026" into the current display format "May 20 2026".
// Unparseable values pass through untouched.
func canonicalBlogDisplayDate(value string) string {
	if t, ok := parseSEODate(strings.TrimSpace(value)); ok {
		return t.Format("January 2 2006")
	}
	return value
}

func normalizeBlogDisplayDates(blog *BlogPost) {
	if blog == nil {
		return
	}
	blog.DatePublished = canonicalBlogDisplayDate(blog.DatePublished)
	blog.LastUpdated = canonicalBlogDisplayDate(blog.LastUpdated)
}

func wrapMarkdownTables(rendered string) string {
	rendered = strings.ReplaceAll(rendered, "<table>", `<div class="markdown-table-wrap"><table>`)
	rendered = strings.ReplaceAll(rendered, "</table>", "</table></div>")
	return rendered
}

func renderMarkdown(markdown string) (template.HTML, error) {
	var out bytes.Buffer
	if err := blogMarkdown.Convert([]byte(markdown), &out); err != nil {
		return "", err
	}
	return template.HTML(wrapMarkdownTables(out.String())), nil
}

var (
	postHeadingRe = regexp.MustCompile(`(?s)<h[23][^>]*>.*?</h[23]>`)
	postHTMLTagRe = regexp.MustCompile(`<[^>]+>`)
	postFenceRe   = regexp.MustCompile("(?s)```.*?```|~~~.*?~~~")
	postImgRe     = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	postLinkRe    = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	postATXRe     = regexp.MustCompile(`(?m)^\s{0,3}#{1,6}\s+`)
	postQuoteRe   = regexp.MustCompile(`(?m)^\s{0,3}>\s?`)
	postListRe    = regexp.MustCompile(`(?m)^\s*([-*+]|\d+\.)\s+`)
	postRuleRe    = regexp.MustCompile(`(?m)^\s*(\*\*\*|---|___)\s*$`)
)

func stripMarkdownToText(md string) string {
	s := postFenceRe.ReplaceAllString(md, " ")
	s = postImgRe.ReplaceAllString(s, "$1")
	s = postLinkRe.ReplaceAllString(s, "$1")
	s = postATXRe.ReplaceAllString(s, "")
	s = postQuoteRe.ReplaceAllString(s, "")
	s = postListRe.ReplaceAllString(s, "")
	s = postRuleRe.ReplaceAllString(s, " ")
	s = postHTMLTagRe.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "*", "")
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, "~", "")
	s = strings.ReplaceAll(s, "#", "")
	s = strings.ReplaceAll(s, "|", " ")
	return strings.Join(strings.Fields(s), " ")
}

func countWords(md string) int {
	return len(strings.Fields(stripMarkdownToText(md)))
}

func readingTimeMinutes(words int) int {
	if words <= 0 {
		return 1
	}
	mins := (words + 199) / 200
	if mins < 1 {
		return 1
	}
	return mins
}

func formatWordCount(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	s := fmt.Sprintf("%d", n)
	out := make([]byte, 0, len(s)+2)
	rem := len(s) % 3
	if rem == 0 {
		rem = 3
	}
	out = append(out, s[:rem]...)
	for i := rem; i < len(s); i += 3 {
		out = append(out, ',')
		out = append(out, s[i:i+3]...)
	}
	return string(out)
}

func stripPostHTMLTags(s string) string {
	return postHTMLTagRe.ReplaceAllString(s, "")
}

// tocFromRenderedHTML builds a table of contents from the rendered h2/h3
// elements and injects stable id attributes back into the HTML so the
// template links and the rendered headings always agree.
func tocFromRenderedHTML(rendered string) ([]tocEntry, string) {
	matches := postHeadingRe.FindAllString(rendered, -1)
	if len(matches) == 0 {
		return nil, rendered
	}
	if len(matches) > 20 {
		matches = matches[:20]
	}
	seen := make(map[string]int, len(matches))
	headings := make([]tocEntry, 0, len(matches))
	out := rendered
	for _, m := range matches {
		level := 2
		if strings.HasPrefix(m, "<h3") {
			level = 3
		}
		gt := strings.Index(m, ">")
		end := strings.LastIndex(m, "</h")
		if gt < 0 || end < 0 || end <= gt {
			continue
		}
		inner := m[gt+1 : end]
		plain := strings.Join(strings.Fields(stripPostHTMLTags(inner)), " ")
		if plain == "" {
			continue
		}
		base := normalizeSlug(plain)
		if base == "" {
			base = "section"
		}
		id := base
		if n := seen[base]; n > 0 {
			id = fmt.Sprintf("%s-%d", base, n+1)
		}
		seen[base]++
		headings = append(headings, tocEntry{Level: level, Text: plain, ID: id})
		var replacement string
		if level == 3 {
			replacement = `<h3 id="` + id + `">` + inner + `</h3>`
		} else {
			replacement = `<h2 id="` + id + `">` + inner + `</h2>`
		}
		out = strings.Replace(out, m, replacement, 1)
	}
	return headings, out
}

func publishedViewPost(b BlogPost) viewPost {
	return viewPost{
		Title:         b.Title,
		Description:   b.Description,
		Category:      b.Category,
		DatePublished: b.DatePublished,
		Thumbnail:     b.Thumbnail,
		Link:          "/blog/" + url.PathEscape(b.Slug),
		ReadingTime:   readingTimeMinutes(countWords(b.Markdown)),
	}
}

// findAdjacentPost returns the newer (newer=true) or older (newer=false)
// published post relative to current, or nil when there isn't one.
func findAdjacentPost(ctx context.Context, current BlogPost, newer bool) *viewPost {
	if current.ID.IsZero() {
		return nil
	}
	var cmp bson.M
	sortDir := 1
	if newer {
		cmp = bson.M{"$gt": current.ID}
		sortDir = 1
	} else {
		cmp = bson.M{"$lt": current.ID}
		sortDir = -1
	}
	filter := bson.M{
		"$and": []bson.M{
			publishedBlogFilter(),
			{"_id": cmp},
		},
	}
	var b BlogPost
	err := blogCollection.FindOne(ctx, filter, options.FindOne().SetSort(bson.D{{Key: "_id", Value: sortDir}})).Decode(&b)
	if err != nil {
		return nil
	}
	if err := ensureBlogSlug(ctx, &b); err != nil {
		return nil
	}
	vp := publishedViewPost(b)
	return &vp
}

// findRelatedPosts returns up to limit recent published posts excluding the
// current one, preferring the same category first.
func findRelatedPosts(ctx context.Context, current BlogPost, limit int) []viewPost {
	if limit <= 0 {
		return nil
	}
	filter := bson.M{
		"$and": []bson.M{
			publishedBlogFilter(),
			{"_id": bson.M{"$ne": current.ID}},
		},
	}
	cur, err := blogCollection.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(int64(limit+3)))
	if err != nil {
		return nil
	}
	defer cur.Close(ctx)
	var posts []BlogPost
	if err := cur.All(ctx, &posts); err != nil {
		return nil
	}
	same := make([]BlogPost, 0, len(posts))
	other := make([]BlogPost, 0, len(posts))
	for i := range posts {
		if err := ensureBlogSlug(ctx, &posts[i]); err != nil {
			continue
		}
		if current.Category != "" && posts[i].Category == current.Category {
			same = append(same, posts[i])
		} else {
			other = append(other, posts[i])
		}
	}
	ordered := append(same, other...)
	if len(ordered) > limit {
		ordered = ordered[:limit]
	}
	out := make([]viewPost, 0, len(ordered))
	for _, b := range ordered {
		out = append(out, publishedViewPost(b))
	}
	return out
}
