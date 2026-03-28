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
	"strings"
	"time"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

var (
	errMissingSlug   = errors.New("slug is required")
	errDuplicateSlug = errors.New("slug already exists")
	errInvalidStatus = errors.New("invalid blog status")
)

const (
	blogStatusDraft     = "draft"
	blogStatusPublished = "published"
)

type viewPost struct {
	Title         string
	Description   string
	Category      string
	DatePublished string
	Thumbnail     string
	Link          string
}

type blogListPageData struct {
	Posts           []viewPost
	PageDescription string
	CanonicalURL    string
	OGImageURL      string
}

type blogPageData struct {
	Title         string
	Description   string
	Category      string
	DatePublished string
	LastUpdated   string
	Thumbnail     string
	CanonicalURL  string
	OGImageURL    string
	JSONLD        template.JS
	Content       template.HTML
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

	cur, err := blogCollection.Find(ctx, publishedBlogFilter())
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
		view = append(view, viewPost{
			Title:         posts[i].Title,
			Description:   posts[i].Description,
			Category:      posts[i].Category,
			DatePublished: posts[i].DatePublished,
			Thumbnail:     posts[i].Thumbnail,
			Link:          "/blog/" + url.PathEscape(posts[i].Slug),
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

	content, err := renderMarkdown(post.Markdown)
	if err != nil {
		http.Error(w, "failed to render post", http.StatusInternalServerError)
		log.Printf("blogPostPageHandler: markdown error: %v", err)
		return
	}

	page := blogPageData{
		Title:         post.Title,
		Description:   post.Description,
		Category:      post.Category,
		DatePublished: post.DatePublished,
		LastUpdated:   post.LastUpdated,
		Thumbnail:     post.Thumbnail,
		CanonicalURL:  absoluteURL(r, "/blog/"+url.PathEscape(post.Slug)),
		OGImageURL:    absoluteAssetURL(r, post.Thumbnail, "/img/post.avif"),
		JSONLD:        articleJSONLD(r, post),
		Content:       content,
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

	cursor, err := blogCollection.Find(ctx, publishedBlogFilter())
	if err != nil {
		http.Error(w, "Error fetching blog posts: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var blogs []BlogPost
	if err := cursor.All(ctx, &blogs); err != nil {
		http.Error(w, "Error decoding blog posts: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if blogs == nil {
		blogs = []BlogPost{}
	}
	for i := range blogs {
		if err := ensureBlogSlug(ctx, &blogs[i]); err != nil {
			http.Error(w, "Error preparing blog posts: "+err.Error(), http.StatusInternalServerError)
			return
		}
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
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
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
			http.Error(w, "failed to insert blog post: "+err.Error(), http.StatusInternalServerError)
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
		http.Error(w, "failed to delete blogposts: "+err.Error(), http.StatusInternalServerError)
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

	if _, ok := updates["markdown"]; ok {
		updates["last_updated"] = time.Now().Format("January 2 2006")
	}

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
		if allowed[k] {
			if s, isStr := v.(string); isStr {
				if s != "" {
					updates[k] = s
				}
			} else {
				updates[k] = v
			}
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
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		}
		return
	}

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
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}

	if err := ensureBlogSlug(ctx, &blog); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(blog)
}

func searchBlogHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed (only GET)"}`, http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, `{"error":"q query param required"}`, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	filter := bson.M{
		"$and": []bson.M{
			publishedBlogFilter(),
			{
				"$or": []bson.M{
					{"title": bson.M{"$regex": q, "$options": "i"}},
					{"description": bson.M{"$regex": q, "$options": "i"}},
					{"category": bson.M{"$regex": q, "$options": "i"}},
					{"slug": bson.M{"$regex": q, "$options": "i"}},
				},
			},
		},
	}

	cursor, err := blogCollection.Find(ctx, filter)
	if err != nil {
		http.Error(w, `{"error":"search failed: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	defer cursor.Close(ctx)

	var results []BlogPost
	if err := cursor.All(ctx, &results); err != nil {
		http.Error(w, `{"error":"decode failed: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	for i := range results {
		if err := ensureBlogSlug(ctx, &results[i]); err != nil {
			http.Error(w, `{"error":"slug preparation failed: `+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
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

func renderMarkdown(markdown string) (template.HTML, error) {
	var out bytes.Buffer
	if err := blogMarkdown.Convert([]byte(markdown), &out); err != nil {
		return "", err
	}
	return template.HTML(out.String()), nil
}
