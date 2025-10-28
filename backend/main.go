package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
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
