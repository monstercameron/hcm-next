package database

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Database struct {
	client *mongo.Client
	db     *mongo.Database
}

// NewDatabase creates a new Database instance
func NewDatabase(uri, dbName string) (*Database, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}

	return &Database{
		client: client,
		db:     client.Database(dbName),
	}, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return d.client.Disconnect(ctx)
}

// InsertOne inserts a single document into the specified collection
func (d *Database) InsertOne(collection string, document interface{}) (*mongo.InsertOneResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := d.db.Collection(collection)
	return coll.InsertOne(ctx, document)
}

// FindOne finds a single document in the specified collection
func (d *Database) FindOne(collection string, filter bson.M, result interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := d.db.Collection(collection)
	return coll.FindOne(ctx, filter).Decode(result)
}

// FindMany finds multiple documents in the specified collection
func (d *Database) FindMany(collection string, filter bson.M) (*mongo.Cursor, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := d.db.Collection(collection)
	return coll.Find(ctx, filter)
}

// UpdateOne updates a single document in the specified collection
func (d *Database) UpdateOne(collection string, filter bson.M, update bson.M) (*mongo.UpdateResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := d.db.Collection(collection)
	return coll.UpdateOne(ctx, filter, update)
}

// DeleteOne deletes a single document from the specified collection
func (d *Database) DeleteOne(collection string, filter bson.M) (*mongo.DeleteResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := d.db.Collection(collection)
	return coll.DeleteOne(ctx, filter)
}

// CountDocuments counts the number of documents in the specified collection
func (d *Database) CountDocuments(collection string, filter bson.M) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := d.db.Collection(collection)
	return coll.CountDocuments(ctx, filter)
}