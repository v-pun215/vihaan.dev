package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type projectPayload struct {
	Title        string   `json:"title"`
	Thumbnail    string   `json:"thumbnail"`
	ThumbnailURL string   `json:"thumbnail_url"`
	DateStart    string   `json:"date_start"`
	StartDate    string   `json:"start_date"`
	DateEnd      string   `json:"date_end"`
	EndDate      string   `json:"end_date"`
	Tags         []string `json:"tags"`
	Description  string   `json:"description"`
	GithubLink   string   `json:"github_link"`
	WebsiteLink  string   `json:"website_link"`
}

func normalizeProjectTags(tags []string) []string {
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		normalized = append(normalized, tag)
	}
	if normalized == nil {
		return []string{}
	}
	return normalized
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func normalizeProject(project Project) Project {
	project.Title = strings.TrimSpace(project.Title)
	project.Thumbnail = strings.TrimSpace(project.Thumbnail)
	project.DateStart = strings.TrimSpace(project.DateStart)
	project.DateEnd = strings.TrimSpace(project.DateEnd)
	project.Description = strings.TrimSpace(project.Description)
	project.GithubLink = strings.TrimSpace(project.GithubLink)
	project.WebsiteLink = strings.TrimSpace(project.WebsiteLink)
	project.Tags = normalizeProjectTags(project.Tags)
	return project
}

func stringFromDocument(doc bson.M, keys ...string) string {
	for _, key := range keys {
		raw, ok := doc[key]
		if !ok || raw == nil {
			continue
		}
		switch value := raw.(type) {
		case string:
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

func objectIDFromDocument(doc bson.M, key string) primitive.ObjectID {
	raw, ok := doc[key]
	if !ok || raw == nil {
		return primitive.NilObjectID
	}
	if oid, ok := raw.(primitive.ObjectID); ok {
		return oid
	}
	return primitive.NilObjectID
}

func intFromDocument(doc bson.M, keys ...string) (int, bool) {
	for _, key := range keys {
		raw, ok := doc[key]
		if !ok || raw == nil {
			continue
		}
		switch value := raw.(type) {
		case int:
			return value, true
		case int32:
			return int(value), true
		case int64:
			return int(value), true
		case float64:
			return int(value), true
		}
	}
	return 0, false
}

func tagsFromDocument(doc bson.M, keys ...string) []string {
	for _, key := range keys {
		raw, ok := doc[key]
		if !ok || raw == nil {
			continue
		}
		switch value := raw.(type) {
		case []string:
			return normalizeProjectTags(value)
		case primitive.A:
			tags := make([]string, 0, len(value))
			for _, item := range value {
				if text, ok := item.(string); ok {
					tags = append(tags, text)
				}
			}
			return normalizeProjectTags(tags)
		case []interface{}:
			tags := make([]string, 0, len(value))
			for _, item := range value {
				if text, ok := item.(string); ok {
					tags = append(tags, text)
				}
			}
			return normalizeProjectTags(tags)
		case string:
			return normalizeProjectTags(strings.Split(value, ","))
		}
	}
	return []string{}
}

func projectFromDocument(doc bson.M) Project {
	displayOrder, _ := intFromDocument(doc, "display_order", "sort_order", "order")
	return normalizeProject(Project{
		ID:           objectIDFromDocument(doc, "_id"),
		DisplayOrder: displayOrder,
		Title:        stringFromDocument(doc, "title"),
		Thumbnail:    stringFromDocument(doc, "thumbnail", "thumbnail_url", "image", "image_url"),
		DateStart:    stringFromDocument(doc, "date_start", "start_date", "start"),
		DateEnd:      stringFromDocument(doc, "date_end", "end_date", "end"),
		Tags:         tagsFromDocument(doc, "tags", "project_tags"),
		Description:  stringFromDocument(doc, "description"),
		GithubLink:   stringFromDocument(doc, "github_link", "github"),
		WebsiteLink:  stringFromDocument(doc, "website_link", "website", "url"),
	})
}

func decodeProjectPayload(r *http.Request) (Project, error) {
	var payload projectPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return Project{}, err
	}

	return normalizeProject(Project{
		Title:       payload.Title,
		Thumbnail:   firstNonEmpty(payload.Thumbnail, payload.ThumbnailURL),
		DateStart:   firstNonEmpty(payload.DateStart, payload.StartDate),
		DateEnd:     firstNonEmpty(payload.DateEnd, payload.EndDate),
		Tags:        payload.Tags,
		Description: payload.Description,
		GithubLink:  payload.GithubLink,
		WebsiteLink: payload.WebsiteLink,
	}), nil
}

type projectOrderRecord struct {
	ID           primitive.ObjectID
	DisplayOrder int
	SourceIndex  int
}

func ensureProjectDisplayOrder(ctx context.Context) error {
	cursor, err := projectCollection.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}))
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	var rawProjects []bson.M
	if err := cursor.All(ctx, &rawProjects); err != nil {
		return err
	}
	if len(rawProjects) == 0 {
		return nil
	}

	allRecords := make([]projectOrderRecord, 0, len(rawProjects))
	explicitRecords := make([]projectOrderRecord, 0, len(rawProjects))
	missingRecords := make([]projectOrderRecord, 0, len(rawProjects))

	for index, doc := range rawProjects {
		record := projectOrderRecord{
			ID:          objectIDFromDocument(doc, "_id"),
			SourceIndex: index,
		}
		if record.ID == primitive.NilObjectID {
			continue
		}
		if order, ok := intFromDocument(doc, "display_order", "sort_order", "order"); ok {
			record.DisplayOrder = order
			explicitRecords = append(explicitRecords, record)
		} else {
			missingRecords = append(missingRecords, record)
		}
		allRecords = append(allRecords, record)
	}

	orderedRecords := make([]projectOrderRecord, 0, len(allRecords))
	if len(missingRecords) == 0 {
		sort.SliceStable(explicitRecords, func(i, j int) bool {
			if explicitRecords[i].DisplayOrder == explicitRecords[j].DisplayOrder {
				return explicitRecords[i].SourceIndex < explicitRecords[j].SourceIndex
			}
			return explicitRecords[i].DisplayOrder < explicitRecords[j].DisplayOrder
		})

		needsNormalization := false
		for index, record := range explicitRecords {
			if record.DisplayOrder != index {
				needsNormalization = true
				break
			}
		}
		if !needsNormalization {
			return nil
		}
		orderedRecords = append(orderedRecords, explicitRecords...)
	} else if len(explicitRecords) == 0 {
		orderedRecords = append(orderedRecords, allRecords...)
	} else {
		sort.SliceStable(explicitRecords, func(i, j int) bool {
			if explicitRecords[i].DisplayOrder == explicitRecords[j].DisplayOrder {
				return explicitRecords[i].SourceIndex < explicitRecords[j].SourceIndex
			}
			return explicitRecords[i].DisplayOrder < explicitRecords[j].DisplayOrder
		})
		orderedRecords = append(orderedRecords, explicitRecords...)
		orderedRecords = append(orderedRecords, missingRecords...)
	}

	models := make([]mongo.WriteModel, 0, len(orderedRecords))
	for index, record := range orderedRecords {
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"_id": record.ID}).
			SetUpdate(bson.M{"$set": bson.M{"display_order": index}}))
	}
	if len(models) == 0 {
		return nil
	}

	_, err = projectCollection.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(true))
	return err
}

func findAllProjects(ctx context.Context) ([]Project, error) {
	if err := ensureProjectDisplayOrder(ctx); err != nil {
		return nil, err
	}

	cursor, err := projectCollection.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{
		{Key: "display_order", Value: 1},
		{Key: "_id", Value: -1},
	}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var rawProjects []bson.M
	if err := cursor.All(ctx, &rawProjects); err != nil {
		return nil, err
	}

	projects := make([]Project, 0, len(rawProjects))
	for _, doc := range rawProjects {
		projects = append(projects, projectFromDocument(doc))
	}
	return projects, nil
}

func nextProjectDisplayOrder(ctx context.Context) (int, error) {
	if err := ensureProjectDisplayOrder(ctx); err != nil {
		return 0, err
	}

	count, err := projectCollection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return 0, err
	}
	return int(count), nil
}

func reorderProjects(ctx context.Context, orderedIDs []string) error {
	if err := ensureProjectDisplayOrder(ctx); err != nil {
		return err
	}

	count, err := projectCollection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return err
	}
	if int(count) != len(orderedIDs) {
		return fmt.Errorf("project reorder payload must include every project")
	}
	if len(orderedIDs) == 0 {
		return nil
	}

	seen := make(map[primitive.ObjectID]struct{}, len(orderedIDs))
	models := make([]mongo.WriteModel, 0, len(orderedIDs))
	for index, idHex := range orderedIDs {
		oid, err := primitive.ObjectIDFromHex(idHex)
		if err != nil {
			return fmt.Errorf("invalid project id: %w", err)
		}
		if _, exists := seen[oid]; exists {
			return fmt.Errorf("duplicate project id in reorder payload")
		}
		seen[oid] = struct{}{}

		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(bson.M{"_id": oid}).
			SetUpdate(bson.M{"$set": bson.M{"display_order": index}}))
	}

	result, err := projectCollection.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(true))
	if err != nil {
		return err
	}
	if result.MatchedCount != int64(len(orderedIDs)) {
		return fmt.Errorf("project reorder payload does not match stored projects")
	}
	return nil
}

func findProjectByID(ctx context.Context, idHex string) (Project, error) {
	oid, err := primitive.ObjectIDFromHex(idHex)
	if err != nil {
		return Project{}, fmt.Errorf("invalid id: %w", err)
	}

	var doc bson.M
	if err := projectCollection.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc); err != nil {
		return Project{}, err
	}
	return projectFromDocument(doc), nil
}

func updateProjectByID(ctx context.Context, idHex string, project Project) error {
	oid, err := primitive.ObjectIDFromHex(idHex)
	if err != nil {
		return fmt.Errorf("invalid id: %w", err)
	}
	project = normalizeProject(project)
	update := bson.M{
		"title":        project.Title,
		"thumbnail":    project.Thumbnail,
		"date_start":   project.DateStart,
		"date_end":     project.DateEnd,
		"tags":         project.Tags,
		"description":  project.Description,
		"github_link":  project.GithubLink,
		"website_link": project.WebsiteLink,
	}
	_, err = projectCollection.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

func deleteProjectByID(ctx context.Context, idHex string) error {
	oid, err := primitive.ObjectIDFromHex(idHex)
	if err != nil {
		return fmt.Errorf("invalid id: %w", err)
	}
	_, err = projectCollection.DeleteOne(ctx, bson.M{"_id": oid})
	return err
}

func allProjectsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	projects, err := findAllProjects(ctx)
	if err != nil {
		http.Error(w, "failed to fetch projects", http.StatusInternalServerError)
		log.Printf("allProjectsHandler: find error: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(projects)
}

func postProjectHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed (only POST)", http.StatusMethodNotAllowed)
		return
	}

	project, err := decodeProjectPayload(r)
	if err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	if project.Title == "" || project.Description == "" {
		http.Error(w, "title and description are required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	project.DisplayOrder, err = nextProjectDisplayOrder(ctx)
	if err != nil {
		http.Error(w, "failed to prepare project order", http.StatusInternalServerError)
		log.Printf("postProjectHandler: order error: %v", err)
		return
	}

	inserted, err := projectCollection.InsertOne(ctx, project)
	if err != nil {
		http.Error(w, "failed to store project", http.StatusInternalServerError)
		log.Printf("postProjectHandler: insert error: %v", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"message":     "project created",
		"inserted_id": fmt.Sprint(inserted.InsertedID),
		"project":     project,
	})
}
func deleteAllProjectsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result, err := projectCollection.DeleteMany(ctx, bson.M{})
	if err != nil {
		log.Printf("deleteAllProjectsHandler: %v", err)
		http.Error(w, "failed to delete projects", http.StatusInternalServerError)
		return
	}

	resp := bson.M{
		"message":       "all projects deleted successfully",
		"deleted_count": result.DeletedCount,
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
