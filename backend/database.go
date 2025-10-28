package main

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// globals you asked to keep
var client *mongo.Client
var blogCollection *mongo.Collection
var projectCollection *mongo.Collection
var pieceCollection *mongo.Collection

// ConnectDB connects to MongoDB using the provided uri and dbName.
// It initializes the global collection variables.
// Caller should provide a context (with timeout) and handle disconnect.
func ConnectDB(ctx context.Context, uri string, dbName string) error {
	if uri == "" {
		return fmt.Errorf("empty mongo uri")
	}
	// Create client options (v1 style)
	clientOpts := options.Client().ApplyURI(uri)

	// Connect
	c, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return err
	}

	// Verify connection
	if err := c.Ping(ctx, nil); err != nil {
		// try to disconnect if ping fails
		_ = c.Disconnect(ctx)
		return err
	}

	// assign globals
	client = c
	db := client.Database(dbName)
	projectCollection = db.Collection("projects")
	pieceCollection = db.Collection("pieces")
	blogCollection = db.Collection("blogposts")

	return nil
}
