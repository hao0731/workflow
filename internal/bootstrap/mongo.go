package bootstrap

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/cheriehsieh/orchestration/internal/config"
)

// MongoConnectFunc establishes a MongoDB client connection.
type MongoConnectFunc func(ctx context.Context, opts *options.ClientOptions) (*mongo.Client, error)

// DefaultMongoConnect opens a MongoDB client with the production driver.
func DefaultMongoConnect(ctx context.Context, opts *options.ClientOptions) (*mongo.Client, error) {
	return mongo.Connect(ctx, opts)
}

// ConnectMongo builds client options from config, opens the client, and returns the selected database.
func ConnectMongo(ctx context.Context, cfg *config.Config, connect MongoConnectFunc) (*mongo.Client, *mongo.Database, error) {
	opts, err := cfg.MongoClientOptions()
	if err != nil {
		return nil, nil, fmt.Errorf("mongo client options: %w", err)
	}

	client, err := connect(ctx, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("connect mongo: %w", err)
	}

	return client, client.Database(cfg.MongoDatabase), nil
}
