package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	adminSessionCookieName = "admin_session"
	adminSessionLifetime   = 7 * 24 * time.Hour
)

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeAdminAPIError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeAdminInternalError(w http.ResponseWriter, err error) {
	log.Printf("admin api: %v", err)
	writeAdminAPIError(w, http.StatusInternalServerError, "internal server error")
}

func adminPassword() (string, error) {
	value := strings.TrimSpace(os.Getenv("ADMIN_PASSWORD"))
	if value == "" {
		return "", fmt.Errorf("ADMIN_PASSWORD is not configured")
	}
	return value, nil
}

func adminSessionSecret() ([]byte, error) {
	value := strings.TrimSpace(os.Getenv("ADMIN_SESSION_SECRET"))
	if value == "" {
		return nil, fmt.Errorf("ADMIN_SESSION_SECRET is not configured")
	}
	return []byte(value), nil
}

func signAdminPayload(secret []byte, payload string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func createAdminSessionValue(expiry time.Time) (string, error) {
	secret, err := adminSessionSecret()
	if err != nil {
		return "", err
	}
	payload := fmt.Sprintf("admin:%d", expiry.Unix())
	signature := signAdminPayload(secret, payload)
	encodedPayload := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return encodedPayload + "." + signature, nil
}

func validateAdminSession(r *http.Request) (bool, error) {
	cookie, err := r.Cookie(adminSessionCookieName)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return false, nil
		}
		return false, err
	}

	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return false, nil
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false, nil
	}
	payload := string(payloadBytes)

	secret, err := adminSessionSecret()
	if err != nil {
		return false, err
	}

	expectedSig := signAdminPayload(secret, payload)
	if subtle.ConstantTimeCompare([]byte(parts[1]), []byte(expectedSig)) != 1 {
		return false, nil
	}

	var expiresAt int64
	if _, err := fmt.Sscanf(payload, "admin:%d", &expiresAt); err != nil {
		return false, nil
	}
	if time.Now().Unix() >= expiresAt {
		return false, nil
	}

	return true, nil
}

func requestIsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]), "https") {
		return true
	}
	return false
}

func setAdminSessionCookie(w http.ResponseWriter, r *http.Request) error {
	value, err := createAdminSessionValue(time.Now().Add(adminSessionLifetime))
	if err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsSecure(r),
		MaxAge:   int(adminSessionLifetime.Seconds()),
	})
	return nil
}

func clearAdminSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func requireAdminPage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, err := validateAdminSession(r)
		if err != nil {
			log.Printf("admin session: %v", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Redirect(w, r, "/admin/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requireAdminAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, err := validateAdminSession(r)
		if err != nil {
			writeAdminInternalError(w, err)
			return
		}
		if !ok {
			writeAdminAPIError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func serveAdminIndex(adminDir string, w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join(adminDir, "index.html"))
}

func adminLoginPageHandler(adminDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ok, err := validateAdminSession(r)
		if err == nil && ok {
			http.Redirect(w, r, "/admin", http.StatusFound)
			return
		}
		serveAdminIndex(adminDir, w, r)
	}
}

func adminDashboardPageHandler(adminDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serveAdminIndex(adminDir, w, r)
	}
}

func adminLoginHandler(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if loginLimiter.isBlocked(ip) {
		writeAdminAPIError(w, http.StatusTooManyRequests, "too many failed login attempts; try again later")
		return
	}

	password, err := adminPassword()
	if err != nil {
		writeAdminInternalError(w, err)
		return
	}

	var payload struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeAdminAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if subtle.ConstantTimeCompare([]byte(payload.Password), []byte(password)) != 1 {
		loginLimiter.recordFailure(ip)
		writeAdminAPIError(w, http.StatusUnauthorized, "invalid password")
		return
	}

	loginLimiter.clear(ip)

	if err := setAdminSessionCookie(w, r); err != nil {
		writeAdminInternalError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}

func adminLogoutHandler(w http.ResponseWriter, r *http.Request) {
	clearAdminSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func adminMeHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"authenticated": true,
		"user":          "admin",
	})
}

func adminMarkdownRenderHandler(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Markdown string `json:"markdown"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeAdminAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	rendered, err := renderMarkdown(payload.Markdown)
	if err != nil {
		writeAdminAPIError(w, http.StatusInternalServerError, "failed to render markdown")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"html": string(rendered)})
}

func normalizeAdminBlog(blog BlogPost) BlogPost {
	blog.Slug = normalizeSlug(blog.Slug)
	blog.Status = normalizeBlogStatus(blog.Status)
	if blog.Status == "" {
		blog.Status = blogStatusDraft
	}
	return blog
}

func adminListBlogsHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	cur, err := blogCollection.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}))
	if err != nil {
		writeAdminAPIError(w, http.StatusInternalServerError, "failed to fetch blogs")
		return
	}
	defer cur.Close(ctx)

	var blogs []BlogPost
	if err := cur.All(ctx, &blogs); err != nil {
		writeAdminAPIError(w, http.StatusInternalServerError, "failed to decode blogs")
		return
	}
	if blogs == nil {
		blogs = []BlogPost{}
	}
	for i := range blogs {
		if err := ensureBlogSlug(ctx, &blogs[i]); err != nil {
			writeAdminAPIError(w, http.StatusInternalServerError, "failed to normalize slug")
			return
		}
		blogs[i] = normalizeAdminBlog(blogs[i])
	}

	writeJSON(w, http.StatusOK, blogs)
}

func adminCreateBlogHandler(w http.ResponseWriter, r *http.Request) {
	var blog BlogPost
	if err := json.NewDecoder(r.Body).Decode(&blog); err != nil {
		writeAdminAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	blog = normalizeAdminBlog(blog)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	id, doc, err := addBlogpost(ctx, blog)
	if err != nil {
		switch {
		case errors.Is(err, errMissingSlug), errors.Is(err, errInvalidStatus):
			writeAdminAPIError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, errDuplicateSlug):
			writeAdminAPIError(w, http.StatusConflict, err.Error())
		default:
			writeAdminInternalError(w, err)
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"inserted_id": id,
		"document":    doc,
	})
}

func adminGetBlogHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	blog, err := findBlogByID(ctx, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			writeAdminAPIError(w, http.StatusNotFound, "blog not found")
			return
		}
		writeAdminAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := ensureBlogSlug(ctx, &blog); err != nil {
		writeAdminInternalError(w, err)
		return
	}
	blog = normalizeAdminBlog(blog)
	writeJSON(w, http.StatusOK, blog)
}

func adminUpdateBlogHandler(w http.ResponseWriter, r *http.Request) {
	var blog BlogPost
	if err := json.NewDecoder(r.Body).Decode(&blog); err != nil {
		writeAdminAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	blog = normalizeAdminBlog(blog)

	updates := bson.M{
		"slug":           blog.Slug,
		"title":          strings.TrimSpace(blog.Title),
		"thumbnail":      strings.TrimSpace(blog.Thumbnail),
		"category":       strings.TrimSpace(blog.Category),
		"date_published": strings.TrimSpace(blog.DatePublished),
		"last_updated":   strings.TrimSpace(blog.LastUpdated),
		"description":    strings.TrimSpace(blog.Description),
		"markdown":       blog.Markdown,
		"status":         blog.Status,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if _, err := updateBlogByID(ctx, r.PathValue("id"), updates); err != nil {
		switch {
		case errors.Is(err, errDuplicateSlug):
			writeAdminAPIError(w, http.StatusConflict, err.Error())
		case errors.Is(err, errInvalidStatus):
			writeAdminAPIError(w, http.StatusBadRequest, err.Error())
		default:
			writeAdminInternalError(w, err)
		}
		return
	}

	adminGetBlogHandler(w, r)
}

func adminDeleteBlogHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	oid, err := primitive.ObjectIDFromHex(r.PathValue("id"))
	if err != nil {
		writeAdminAPIError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if _, err := blogCollection.DeleteOne(ctx, bson.M{"_id": oid}); err != nil {
		writeAdminAPIError(w, http.StatusInternalServerError, "failed to delete blog")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func listAdminProjects(ctx context.Context) ([]Project, error) {
	return findAllProjects(ctx)
}

func adminListProjectsHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	projects, err := listAdminProjects(ctx)
	if err != nil {
		writeAdminAPIError(w, http.StatusInternalServerError, "failed to fetch projects")
		return
	}
	writeJSON(w, http.StatusOK, projects)
}

func adminCreateProjectHandler(w http.ResponseWriter, r *http.Request) {
	project, err := decodeProjectPayload(r)
	if err != nil {
		writeAdminAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if project.Title == "" || project.Description == "" {
		writeAdminAPIError(w, http.StatusBadRequest, "title and description are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	project.DisplayOrder, err = nextProjectDisplayOrder(ctx)
	if err != nil {
		writeAdminAPIError(w, http.StatusInternalServerError, "failed to prepare project order")
		return
	}
	inserted, err := projectCollection.InsertOne(ctx, project)
	if err != nil {
		writeAdminAPIError(w, http.StatusInternalServerError, "failed to create project")
		return
	}
	if oid, ok := inserted.InsertedID.(primitive.ObjectID); ok {
		project.ID = oid
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"inserted_id": inserted.InsertedID,
		"project":     project,
	})
}

func adminReorderProjectsHandler(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeAdminAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := reorderProjects(ctx, payload.IDs); err != nil {
		writeAdminAPIError(w, http.StatusBadRequest, err.Error())
		return
	}

	projects, err := listAdminProjects(ctx)
	if err != nil {
		writeAdminAPIError(w, http.StatusInternalServerError, "failed to fetch reordered projects")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":       true,
		"projects": projects,
	})
}

func adminGetProjectHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	project, err := findProjectByID(ctx, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			writeAdminAPIError(w, http.StatusNotFound, "project not found")
			return
		}
		writeAdminAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func adminUpdateProjectHandler(w http.ResponseWriter, r *http.Request) {
	project, err := decodeProjectPayload(r)
	if err != nil {
		writeAdminAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if project.Title == "" || project.Description == "" {
		writeAdminAPIError(w, http.StatusBadRequest, "title and description are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := updateProjectByID(ctx, r.PathValue("id"), project); err != nil {
		writeAdminAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	adminGetProjectHandler(w, r)
}

func adminDeleteProjectHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := deleteProjectByID(ctx, r.PathValue("id")); err != nil {
		writeAdminAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func adminListPiecesHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	pieces, err := findAllPieces(ctx)
	if err != nil {
		writeAdminAPIError(w, http.StatusInternalServerError, "failed to fetch pieces")
		return
	}
	writeJSON(w, http.StatusOK, pieces)
}

func adminCreatePieceHandler(w http.ResponseWriter, r *http.Request) {
	piece, err := decodePiecePayload(r)
	if err != nil {
		writeAdminAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if piece.Title == "" || piece.Description == "" {
		writeAdminAPIError(w, http.StatusBadRequest, "title and description are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	inserted, err := createPiece(ctx, piece)
	if err != nil {
		writeAdminAPIError(w, http.StatusInternalServerError, "failed to create piece")
		return
	}
	if oid, ok := inserted.(primitive.ObjectID); ok {
		piece.ID = oid
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"inserted_id": inserted,
		"piece":       piece,
	})
}

func adminGetPieceHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	piece, err := findPieceByID(ctx, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			writeAdminAPIError(w, http.StatusNotFound, "piece not found")
			return
		}
		writeAdminAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, piece)
}

func adminUpdatePieceHandler(w http.ResponseWriter, r *http.Request) {
	piece, err := decodePiecePayload(r)
	if err != nil {
		writeAdminAPIError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if piece.Title == "" || piece.Description == "" {
		writeAdminAPIError(w, http.StatusBadRequest, "title and description are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := updatePieceByID(ctx, r.PathValue("id"), piece); err != nil {
		writeAdminAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	adminGetPieceHandler(w, r)
}

func adminDeletePieceHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := deletePieceByID(ctx, r.PathValue("id")); err != nil {
		writeAdminAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
