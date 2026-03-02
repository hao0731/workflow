package config

import (
	"fmt"

	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoClientOptions builds MongoDB client options from configuration.
func (c *Config) MongoClientOptions() (*options.ClientOptions, error) {
	hasUsername := c.MongoUsername != ""
	hasPassword := c.MongoPassword != ""

	if hasUsername != hasPassword {
		return nil, fmt.Errorf(
			"invalid mongo auth config: MONGO_USERNAME and MONGO_PASSWORD must both be set or both be empty",
		)
	}

	opts := options.Client().ApplyURI(c.MongoURI)
	if hasUsername {
		opts.SetAuth(options.Credential{
			Username: c.MongoUsername,
			Password: c.MongoPassword,
		})
	}

	return opts, nil
}
