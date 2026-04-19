package repository

import "go.mongodb.org/mongo-driver/mongo/options"

// mongoUpsert is a tiny helper so repositories don't all have to import the
// options package just for an upsert flag.
func mongoUpsert() *options.UpdateOptions {
	t := true
	return &options.UpdateOptions{Upsert: &t}
}
