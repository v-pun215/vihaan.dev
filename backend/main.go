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
	Title       string `bson:"title,omitempty" json:"title,omitempty"`
	Description string `bson:"description,omitempty" json:"description,omitempty"`
	GithubLink  string `bson:"github_link,omitempty" json:"github_link,omitempty"`
	WebsiteLink string `bson:"website_link,omitempty" json:"website_link,omitempty"`
}

type Piece struct {
	Title       string `bson:"title,omitempty" json:"title,omitempty"`
	VideoURL    string `bson:"video_url,omitempty" json:"video_url,omitempty"`
	Description string `bson:"description,omitempty" json:"description,omitempty"`
}

type BlogPost struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id,omitempty"`
	Title         string             `bson:"title,omitempty" json:"title,omitempty"`
	Thumbnail     string             `bson:"thumbnail,omitempty" json:"thumbnail,omitempty"`
	Category      string             `bson:"category,omitempty" json:"category,omitempty"`
	DatePublished string             `bson:"date_published,omitempty" json:"date_published,omitempty"`
	LastUpdated   string             `bson:"last_updated,omitempty" json:"last_updated,omitempty"`
	Description   string             `bson:"description,omitempty" json:"description,omitempty"`
	Markdown      string             `bson:"markdown,omitempty" json:"markdown,omitempty"`
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
	mux.HandleFunc("/api/blogposts", all_blogs_handler)
	mux.HandleFunc("/api/blogposts/post", postBlogHandler)
	mux.HandleFunc("/api/deleteall", deleteAllHandler)
	mux.HandleFunc("/api/blogposts/edit", editBlogHandler)     // POST
	mux.HandleFunc("/api/blogposts/get", getBlogHandler)       // GET ?id=
	mux.HandleFunc("/api/blogposts/search", searchBlogHandler) // GET ?q=
	staticRoutes(mux, "../static")
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port

	srv := &http.Server{
		Addr:    addr,
		Handler: enableCORS(mux),
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

	fullPath := func(rel string) string {
		return filepath.Join(frontendDir, filepath.FromSlash(rel))
	}
	serve404 := func(w http.ResponseWriter, r *http.Request) {
		notFoundPath := fullPath("404.html")
		if fi, err := os.Stat(notFoundPath); err == nil && !fi.IsDir() {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			http.ServeFile(w, r, notFoundPath)
			return
		}
		http.Error(w, "404 page not found", http.StatusNotFound)
	}
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
	mux.HandleFunc("/blog", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		http.ServeFile(w, r, fullPath("blog.html"))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Do not allow static handler to catch API routes
		if strings.HasPrefix(r.URL.Path, "/api/") {
			serve404(w, r)
			return
		}

		if r.URL.Path == "/" {
			http.ServeFile(w, r, fullPath("index.html"))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/") {
			serve404(w, r)
			return
		}
		rel := strings.TrimPrefix(r.URL.Path, "/")
		if rel == "" {
			serve404(w, r)
			return
		}
		if strings.Contains(rel, "..") {
			serve404(w, r)
			return
		}
		firstSeg := rel
		if idx := strings.Index(rel, "/"); idx != -1 {
			firstSeg = rel[:idx]
		}
		if firstSeg == "js" || firstSeg == "img" {
			serve404(w, r)
			return
		}
		target := fullPath(rel)
		absFrontend, _ := filepath.Abs(frontendDir)
		absTarget, err := filepath.Abs(target)
		if err != nil || !strings.HasPrefix(absTarget, absFrontend) {
			serve404(w, r)
			return
		}
		info, err := os.Stat(target)
		if err != nil || info.IsDir() {
			serve404(w, r)
			return
		}

		http.ServeFile(w, r, target)
	})
}
func enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}
