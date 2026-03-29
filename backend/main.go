package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Project struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	DisplayOrder int                `bson:"display_order,omitempty" json:"display_order,omitempty"`
	Title        string             `bson:"title,omitempty" json:"title,omitempty"`
	Thumbnail    string             `bson:"thumbnail,omitempty" json:"thumbnail,omitempty"`
	DateStart    string             `bson:"date_start,omitempty" json:"date_start,omitempty"`
	DateEnd      string             `bson:"date_end,omitempty" json:"date_end,omitempty"`
	Tags         []string           `bson:"tags,omitempty" json:"tags,omitempty"`
	Description  string             `bson:"description,omitempty" json:"description,omitempty"`
	GithubLink   string             `bson:"github_link,omitempty" json:"github_link,omitempty"`
	WebsiteLink  string             `bson:"website_link,omitempty" json:"website_link,omitempty"`
}

type Piece struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	Title       string             `bson:"title,omitempty" json:"title,omitempty"`
	VideoURL    string             `bson:"video_url,omitempty" json:"video_url,omitempty"`
	Description string             `bson:"description,omitempty" json:"description,omitempty"`
}

type BlogPost struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	Slug          string             `bson:"slug,omitempty" json:"slug,omitempty"`
	Title         string             `bson:"title,omitempty" json:"title,omitempty"`
	Thumbnail     string             `bson:"thumbnail,omitempty" json:"thumbnail,omitempty"`
	Category      string             `bson:"category,omitempty" json:"category,omitempty"`
	DatePublished string             `bson:"date_published,omitempty" json:"date_published,omitempty"`
	LastUpdated   string             `bson:"last_updated,omitempty" json:"last_updated,omitempty"`
	Description   string             `bson:"description,omitempty" json:"description,omitempty"`
	Markdown      string             `bson:"markdown,omitempty" json:"markdown,omitempty"`
	Status        string             `bson:"status,omitempty" json:"status,omitempty"`
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println(".env not found")
	}

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		log.Fatal("MONGO_URI not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := ConnectDB(ctx, mongoURI, "main"); err != nil {
		log.Fatalf("failed to connect to MongoDB: %v", err)
	}
	defer func() {
		discCtx, discCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer discCancel()
		if err := client.Disconnect(discCtx); err != nil {
			log.Printf("error disconnecting mongo client: %v", err)
		}
	}()

	mux := http.NewServeMux()
	adminDir := "../static/admin"
	mux.Handle("GET /admin/assets/", http.StripPrefix("/admin/assets/", http.FileServer(http.Dir(adminDir))))
	mux.HandleFunc("GET /admin/login", adminLoginPageHandler(adminDir))
	mux.Handle("GET /admin", requireAdminPage(http.HandlerFunc(adminDashboardPageHandler(adminDir))))
	mux.Handle("GET /admin/", requireAdminPage(http.HandlerFunc(adminDashboardPageHandler(adminDir))))

	mux.HandleFunc("POST /api/admin/login", adminLoginHandler)
	mux.Handle("POST /api/admin/logout", requireAdminAPI(http.HandlerFunc(adminLogoutHandler)))
	mux.Handle("GET /api/admin/me", requireAdminAPI(http.HandlerFunc(adminMeHandler)))
	mux.Handle("POST /api/admin/markdown/render", requireAdminAPI(http.HandlerFunc(adminMarkdownRenderHandler)))
	mux.Handle("GET /api/admin/blogs", requireAdminAPI(http.HandlerFunc(adminListBlogsHandler)))
	mux.Handle("POST /api/admin/blogs", requireAdminAPI(http.HandlerFunc(adminCreateBlogHandler)))
	mux.Handle("GET /api/admin/blogs/{id}", requireAdminAPI(http.HandlerFunc(adminGetBlogHandler)))
	mux.Handle("PUT /api/admin/blogs/{id}", requireAdminAPI(http.HandlerFunc(adminUpdateBlogHandler)))
	mux.Handle("DELETE /api/admin/blogs/{id}", requireAdminAPI(http.HandlerFunc(adminDeleteBlogHandler)))
	mux.Handle("GET /api/admin/projects", requireAdminAPI(http.HandlerFunc(adminListProjectsHandler)))
	mux.Handle("POST /api/admin/projects", requireAdminAPI(http.HandlerFunc(adminCreateProjectHandler)))
	mux.Handle("POST /api/admin/projects/reorder", requireAdminAPI(http.HandlerFunc(adminReorderProjectsHandler)))
	mux.Handle("GET /api/admin/projects/{id}", requireAdminAPI(http.HandlerFunc(adminGetProjectHandler)))
	mux.Handle("PUT /api/admin/projects/{id}", requireAdminAPI(http.HandlerFunc(adminUpdateProjectHandler)))
	mux.Handle("DELETE /api/admin/projects/{id}", requireAdminAPI(http.HandlerFunc(adminDeleteProjectHandler)))
	mux.Handle("GET /api/admin/pieces", requireAdminAPI(http.HandlerFunc(adminListPiecesHandler)))
	mux.Handle("POST /api/admin/pieces", requireAdminAPI(http.HandlerFunc(adminCreatePieceHandler)))
	mux.Handle("GET /api/admin/pieces/{id}", requireAdminAPI(http.HandlerFunc(adminGetPieceHandler)))
	mux.Handle("PUT /api/admin/pieces/{id}", requireAdminAPI(http.HandlerFunc(adminUpdatePieceHandler)))
	mux.Handle("DELETE /api/admin/pieces/{id}", requireAdminAPI(http.HandlerFunc(adminDeletePieceHandler)))

	mux.HandleFunc("/api/projects", allProjectsHandler)
	mux.HandleFunc("/api/pieces", allPiecesHandler)
	mux.Handle("/api/projects/post", requireAdminAPI(http.HandlerFunc(postProjectHandler)))
	mux.Handle("/api/projects/deleteall", requireAdminAPI(http.HandlerFunc(deleteAllProjectsHandler)))
	mux.HandleFunc("/api/blogposts", all_blogs_handler)
	mux.Handle("/api/blogposts/post", requireAdminAPI(http.HandlerFunc(postBlogHandler)))
	mux.Handle("/api/deleteall", requireAdminAPI(http.HandlerFunc(deleteAllHandler)))
	mux.Handle("/api/blogposts/edit", requireAdminAPI(http.HandlerFunc(editBlogHandler)))
	mux.HandleFunc("/api/blogposts/get", getBlogHandler)
	mux.HandleFunc("/api/blogposts/search", searchBlogHandler)
	mux.HandleFunc("/robots.txt", robotsHandler)
	mux.HandleFunc("/sitemap.xml", sitemapHandler)
	mux.HandleFunc("/blog/", blogPostPageHandler)
	// register server-side rendered blog list BEFORE static routes
	mux.HandleFunc("/blog", blogListHandler)

	staticRoutes(mux, "../static")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port

	srv := &http.Server{
		Addr:              addr,
		Handler:           securityHeaders(noindexAdminRoutes(enableCORS(mux))),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// graceful shutdown handling
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Server listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server listen error: %v", err)
		}
	}()

	<-stop
	log.Println("Shutting down server...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown error: %v", err)
	}
}

func staticRoutes(mux *http.ServeMux, frontendDir string) {
	if info, err := os.Stat(frontendDir); err != nil || !info.IsDir() {
		log.Printf("staticRoutes: frontend directory not found: %s", frontendDir)
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "frontend not found", http.StatusInternalServerError)
		})
		return
	}

	absFrontend, err := filepath.Abs(frontendDir)
	if err != nil {
		log.Fatalf("failed to get absolute path of frontend dir: %v", err)
	}

	fullPath := func(rel string) string {
		return filepath.Join(frontendDir, filepath.FromSlash(rel))
	}

	serve404 := func(w http.ResponseWriter, r *http.Request) {
		notFoundPath := fullPath("404.html")
		content, err := os.ReadFile(notFoundPath)
		if err != nil {
			http.Error(w, "404 page not found", http.StatusNotFound)
			return
		}

		html := string(content)

		// Inject base tag if not already present
		if !strings.Contains(strings.ToLower(html), "<base") {
			// Find <head> tag and inject <base href="/"> after it
			headIdx := strings.Index(strings.ToLower(html), "<head>")
			if headIdx != -1 {
				insertPos := headIdx + 6 // length of "<head>"
				html = html[:insertPos] + "\n  <base href=\"/\">" + html[insertPos:]
			} else {
				// If no <head> tag, try after <!DOCTYPE> or at the beginning
				doctypeIdx := strings.Index(strings.ToLower(html), "<!doctype")
				if doctypeIdx != -1 {
					// Find end of doctype
					endIdx := strings.Index(html[doctypeIdx:], ">")
					if endIdx != -1 {
						insertPos := doctypeIdx + endIdx + 1
						html = html[:insertPos] + "\n<base href=\"/\">" + html[insertPos:]
					}
				}
			}
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(html))
	}

	// Specific HTML page routes
	mux.HandleFunc("/projects", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		http.ServeFile(w, r, fullPath("projects.html"))
	})

	mux.HandleFunc("/pieces", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		http.ServeFile(w, r, fullPath("pieces.html"))
	})

	// Catch-all handler for static files and root
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Block API routes from being handled here
		if strings.HasPrefix(r.URL.Path, "/api/") {
			serve404(w, r)
			return
		}

		// Serve index.html for root
		if r.URL.Path == "/" {
			http.ServeFile(w, r, fullPath("index.html"))
			return
		}

		// Reject paths ending with /
		if strings.HasSuffix(r.URL.Path, "/") {
			serve404(w, r)
			return
		}

		// Clean the path
		rel := strings.TrimPrefix(r.URL.Path, "/")
		if rel == "" {
			serve404(w, r)
			return
		}

		// Prevent path traversal
		if strings.Contains(rel, "..") {
			serve404(w, r)
			return
		}

		// Build target path
		target := fullPath(rel)
		absTarget, err := filepath.Abs(target)
		if err != nil || !strings.HasPrefix(absTarget, absFrontend) {
			serve404(w, r)
			return
		}

		// Check if file exists and is not a directory
		info, err := os.Stat(target)
		if err != nil {
			serve404(w, r)
			return
		}

		// Don't serve directories
		if info.IsDir() {
			serve404(w, r)
			return
		}

		// Serve the static file
		http.ServeFile(w, r, target)
	})
}
