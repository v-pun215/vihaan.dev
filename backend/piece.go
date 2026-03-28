package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type piecePayload struct {
	Title       string `json:"title"`
	VideoURL    string `json:"video_url"`
	Description string `json:"description"`
}

func normalizePiece(piece Piece) Piece {
	piece.Title = strings.TrimSpace(piece.Title)
	piece.VideoURL = strings.TrimSpace(piece.VideoURL)
	piece.Description = strings.TrimSpace(piece.Description)
	return piece
}

func pieceFromDocument(doc bson.M) Piece {
	return normalizePiece(Piece{
		ID:          objectIDFromDocument(doc, "_id"),
		Title:       stringFromDocument(doc, "title", "name"),
		VideoURL:    stringFromDocument(doc, "video_url", "video", "url", "link"),
		Description: stringFromDocument(doc, "description", "summary", "notes"),
	})
}

func decodePiecePayload(r *http.Request) (Piece, error) {
	var payload piecePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return Piece{}, err
	}

	return normalizePiece(Piece{
		Title:       payload.Title,
		VideoURL:    payload.VideoURL,
		Description: payload.Description,
	}), nil
}

func findAllPieces(ctx context.Context) ([]Piece, error) {
	cur, err := pieceCollection.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var rawPieces []bson.M
	if err := cur.All(ctx, &rawPieces); err != nil {
		return nil, err
	}

	pieces := make([]Piece, 0, len(rawPieces))
	for _, doc := range rawPieces {
		pieces = append(pieces, pieceFromDocument(doc))
	}
	if pieces == nil {
		pieces = []Piece{}
	}
	return pieces, nil
}

func findPieceByID(ctx context.Context, idHex string) (Piece, error) {
	oid, err := primitive.ObjectIDFromHex(idHex)
	if err != nil {
		return Piece{}, fmt.Errorf("invalid id: %w", err)
	}

	var doc bson.M
	if err := pieceCollection.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc); err != nil {
		return Piece{}, err
	}
	return pieceFromDocument(doc), nil
}

func createPiece(ctx context.Context, piece Piece) (interface{}, error) {
	piece = normalizePiece(piece)
	result, err := pieceCollection.InsertOne(ctx, piece)
	if err != nil {
		return nil, err
	}
	return result.InsertedID, nil
}

func updatePieceByID(ctx context.Context, idHex string, piece Piece) error {
	oid, err := primitive.ObjectIDFromHex(idHex)
	if err != nil {
		return fmt.Errorf("invalid id: %w", err)
	}
	piece = normalizePiece(piece)
	update := map[string]interface{}{
		"title":       piece.Title,
		"video_url":   piece.VideoURL,
		"description": piece.Description,
	}
	_, err = pieceCollection.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

func allPiecesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	pieces, err := findAllPieces(ctx)
	if err != nil {
		http.Error(w, "failed to fetch pieces", http.StatusInternalServerError)
		log.Printf("allPiecesHandler: fetch error: %v", err)
		return
	}

	writeJSON(w, http.StatusOK, pieces)
}

func deletePieceByID(ctx context.Context, idHex string) error {
	oid, err := primitive.ObjectIDFromHex(idHex)
	if err != nil {
		return fmt.Errorf("invalid id: %w", err)
	}
	_, err = pieceCollection.DeleteOne(ctx, bson.M{"_id": oid})
	return err
}
