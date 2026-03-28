package main

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var client *mongo.Client
var blogCollection *mongo.Collection
var projectCollection *mongo.Collection
var pieceCollection *mongo.Collection

func ConnectDB(ctx context.Context, uri string, dbName string) error {
	if uri == "" {
		return fmt.Errorf("empty mongo uri")
	}

	clientOpts := options.Client().ApplyURI(uri)

	c, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return err
	}
	if err := c.Ping(ctx, nil); err != nil {
		_ = c.Disconnect(ctx)
		return err
	}

	client = c
	db := client.Database(dbName)
	projectCollection = db.Collection("projects")
	pieceCollection = db.Collection("pieces")
	blogCollection = db.Collection("blogposts")
	if _, err := blogCollection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "slug", Value: 1}},
	}); err != nil {
		return fmt.Errorf("failed to create slug index: %w", err)
	}

	return nil
}
