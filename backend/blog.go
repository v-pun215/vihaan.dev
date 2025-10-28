package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func all_blogs_handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Fetch all documents from the blogposts collection
	cursor, err := blogCollection.Find(ctx, map[string]interface{}{})
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(blogs)
}
func addBlogpost(ctx context.Context, blog BlogPost) (interface{}, bson.M, error) {
	if blog.Title == "" ||
		blog.Thumbnail == "" ||
		blog.Category == "" ||
		blog.DatePublished == "" ||
		blog.LastUpdated == "" ||
		blog.Description == "" ||
		blog.Markdown == "" {
		return nil, nil, fmt.Errorf("missing required blog fields")
	}

	result, err := blogCollection.InsertOne(ctx, blog)
	blog.ID = result.InsertedID.(primitive.ObjectID)
	if err != nil {
		return nil, nil, err
	}

	doc := bson.M{
		"id":             blog.ID.Hex(),
		"title":          blog.Title,
		"thumbnail":      blog.Thumbnail,
		"category":       blog.Category,
		"date_published": blog.DatePublished,
		"last_updated":   blog.LastUpdated,
		"description":    blog.Description,
		"markdown":       blog.Markdown,
	}

	return result.InsertedID, doc, nil
}
func postBlogHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed (only POST)", http.StatusMethodNotAllowed)
		return
	}

	// Decode JSON body into BlogPost struct
	var blog BlogPost
	if err := json.NewDecoder(r.Body).Decode(&blog); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	id, doc, err := addBlogpost(ctx, blog)
	if err != nil {
		http.Error(w, "failed to insert blog post: "+err.Error(), http.StatusInternalServerError)
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
	res, err := coll.DeleteMany(ctx, bson.M{}) // Empty filter → deletes all docs
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

// deleteAllHandler — HTTP endpoint to trigger deletion (POST for safety)
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
	json.NewEncoder(w).Encode(resp)
}
func updateBlogByID(ctx context.Context, idHex string, updates bson.M) (int64, error) {
	oid, err := primitive.ObjectIDFromHex(idHex)
	if err != nil {
		return 0, fmt.Errorf("invalid id: %w", err)
	}

	if len(updates) == 0 {
		return 0, fmt.Errorf("no updates provided")
	}

	// Ensure we do not allow modifying the _id
	delete(updates, "_id")
	delete(updates, "id")

	// If markdown or other content changed, update LastUpdated to today
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

	// parse JSON body into a generic map
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	// id is required
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

	// Build updates: whitelist allowed fields to avoid accidental injection
	allowed := map[string]bool{
		"title":          true,
		"thumbnail":      true,
		"category":       true,
		"date_published": true,
		"last_updated":   true,
		"description":    true,
		"markdown":       true,
	}
	updates := bson.M{}
	for k, v := range payload {
		if k == "id" {
			continue
		}
		if allowed[k] {
			// Only add non-empty values (so sending empty "" won't wipe fields accidentally)
			if s, isStr := v.(string); isStr {
				if s != "" {
					updates[k] = s
				}
			} else {
				// allow non-strings if needed
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
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(bson.M{
		"modified_count": modified,
	})
}
func getBlogHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed (only GET)"}`, http.StatusMethodNotAllowed)
		return
	}

	idStr := r.URL.Query().Get("id")
	if idStr == "" {
		http.Error(w, `{"error":"id query param required"}`, http.StatusBadRequest)
		return
	}

	oid, err := primitive.ObjectIDFromHex(idStr)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var blog BlogPost
	if err := blogCollection.FindOne(ctx, bson.M{"_id": oid}).Decode(&blog); err != nil {
		http.Error(w, `{"error":"post not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(blog)
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
		"$or": []bson.M{
			{"title": bson.M{"$regex": q, "$options": "i"}},
			{"description": bson.M{"$regex": q, "$options": "i"}},
			{"category": bson.M{"$regex": q, "$options": "i"}},
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}
