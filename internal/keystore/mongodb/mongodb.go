package mongodb

import (
	"context"
	"errors"
	"fmt"
	"github.com/minio/kes"
	kesdk "github.com/minio/kes-go"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
	"time"
)

type Store struct {
	client     *mongo.Client
	collection *mongo.Collection
}

type KesMongoModel struct {
	Id      string    `bson:"_id"`
	Value   []byte    `bson:"value"`
	Created time.Time `bson:"created"`
}

func NewMongoStore(uri string, database string, collectionName string) (*Store, error) {

	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}

	collection := client.Database(database).Collection(collectionName)

	return &Store{
		client:     client,
		collection: collection,
	}, nil
}

func (s Store) Close() error {
	if err := s.client.Disconnect(context.Background()); err != nil {
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
	err := s.client.Ping(context.Background(), readpref.Primary())
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
		Id:      name,
		Value:   value,
		Created: time.Now().UTC(),
	}

	_, err := s.collection.InsertOne(context.Background(), document)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return kesdk.ErrKeyExists
		}

		return err
	}

	return nil
}

func (s Store) Delete(ctx context.Context, name string) error {

	filter := bson.D{{
		"_id", name,
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

func (s Store) Get(ctx context.Context, name string) ([]byte, error) {
	filter := bson.D{{
		"_id", name,
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

func (s Store) List(ctx context.Context, prefix string, n int) ([]string, string, error) {
	filter := bson.D{{"_id", bson.D{{"$regex", fmt.Sprintf("/^%s/", prefix)}}}}

	cursor, err := s.collection.Find(ctx, filter)
	if err != nil {
		return nil, "", err
	}
	defer cursor.Close(ctx)

	cnt := 0
	results := make([]string, n)
	lastPrefix := ""
	for cursor.Next(ctx) {
		var document KesMongoModel
		err := cursor.Decode(&document)
		if err != nil {
			return nil, "", err
		}

		cnt++
		results = append(results, document.Id)
		lastPrefix = document.Id

		if cnt == n {
			break
		}
	}

	return results, lastPrefix, nil
}
