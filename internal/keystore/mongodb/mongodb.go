package mongodb

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/minio/kes"
	kesdk "github.com/minio/kes-go"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// Store is a mongodb secret store
type Store struct {
	client     *mongo.Client
	collection *mongo.Collection
	ctx        context.Context
}

// KesMongoModel is the model for the mongodb database documents
type KesMongoModel struct {
	ID      string    `bson:"_id"`
	Value   []byte    `bson:"value"`
	Created time.Time `bson:"created"`
}

// Config is a structure containing configuration options for connecting to MongoDB.
type Config struct {
	ConnectionString string

	Database string

	Collection string
}

// Connect establishes a connection to mongodb and returns a new Store
func Connect(ctx context.Context, config *Config) (*Store, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(config.ConnectionString))
	if err != nil {
		return nil, err
	}

	collection := client.Database(config.Database).Collection(config.Collection)

	return &Store{
		client:     client,
		collection: collection,
	}, nil
}

// Close closes the connection to mongodb
func (s Store) Close() error {
	if err := s.client.Disconnect(s.ctx); err != nil {
		return err
	}

	return nil
}

// Status returns the current state of the Conn.
//
// In particular, it reports whether the underlying
// mongodb is accessible.
func (s Store) Status(ctx context.Context) (kes.KeyStoreState, error) {
	start := time.Now()
	err := s.client.Ping(s.ctx, readpref.Primary())
	if err != nil {
		return kes.KeyStoreState{}, err
	}

	return kes.KeyStoreState{
		Latency: time.Since(start),
	}, nil
}

// Create creates a new document with the given name as id
//
// It returns kes.ErrKeyExists if the id already exists
func (s Store) Create(ctx context.Context, name string, value []byte) error {
	document := KesMongoModel{
		ID:      name,
		Value:   value,
		Created: time.Now().UTC(),
	}

	_, err := s.collection.InsertOne(s.ctx, document)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return kesdk.ErrKeyExists
		}

		return err
	}

	return nil
}

// Delete deletes the document with the given name as id
func (s Store) Delete(ctx context.Context, name string) error {
	filter := bson.D{{
		Key: "_id", Value: name,
	}}

	deleteOneResult, err := s.collection.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}

	if deleteOneResult.DeletedCount == 0 {
		return kesdk.ErrKeyNotFound
	}

	return nil
}

// Get returns the value associated with the given name
func (s Store) Get(ctx context.Context, name string) ([]byte, error) {
	filter := bson.D{{
		Key: "_id", Value: name,
	}}

	var document KesMongoModel
	err := s.collection.FindOne(ctx, filter).Decode(&document)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, kesdk.ErrKeyNotFound
		}
	}

	return document.Value, nil
}

// List returns a list of keys with the given prefix
func (s Store) List(ctx context.Context, prefix string, n int) ([]string, string, error) {
	filter := bson.D{}
	if len(prefix) > 0 {
		filter = bson.D{{Key: "_id", Value: bson.D{{Key: "$regex", Value: fmt.Sprintf("/^%s/", prefix)}}}}
	}

	findOpts := &options.FindOptions{}
	if n > 0 {
		findOpts.SetLimit(int64(n))
	}

	cursor, err := s.collection.Find(ctx, filter, findOpts)
	if err != nil {
		return nil, "", err
	}
	defer cursor.Close(ctx)

	cnt := 0
	results := make([]string, 0)
	lastPrefix := ""
	for cursor.Next(ctx) {
		var document KesMongoModel
		err := cursor.Decode(&document)
		if err != nil {
			return nil, "", err
		}

		cnt++
		results = append(results, document.ID)
		lastPrefix = document.ID

		if n > 0 && cnt == n {
			break
		}
	}

	return results, lastPrefix, nil
}
