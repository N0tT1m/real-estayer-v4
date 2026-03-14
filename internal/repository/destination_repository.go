package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/realestayer/v3/internal/models"
)

type DestinationRepository struct {
	collection *mongo.Collection
}

func NewDestinationRepository(db *mongo.Database) *DestinationRepository {
	return &DestinationRepository{
		collection: db.Collection("destinations"),
	}
}

func (r *DestinationRepository) Create(ctx context.Context, dest *models.Destination) error {
	dest.CreatedAt = time.Now()
	dest.UpdatedAt = time.Now()
	dest.Active = true

	result, err := r.collection.InsertOne(ctx, dest)
	if err != nil {
		return err
	}

	dest.ID = result.InsertedID.(primitive.ObjectID)
	return nil
}

func (r *DestinationRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*models.Destination, error) {
	var dest models.Destination
	err := r.collection.FindOne(ctx, bson.M{"_id": id, "active": true}).Decode(&dest)
	if err != nil {
		return nil, err
	}
	return &dest, nil
}

func (r *DestinationRepository) FindByName(ctx context.Context, name string) (*models.Destination, error) {
	var dest models.Destination
	err := r.collection.FindOne(ctx, bson.M{"name": name, "active": true}).Decode(&dest)
	if err != nil {
		return nil, err
	}
	return &dest, nil
}

func (r *DestinationRepository) Find(ctx context.Context, filter models.DestinationFilter) ([]models.Destination, int64, error) {
	query := bson.M{"active": true}

	if filter.Category != "" {
		query["categories"] = filter.Category
	}
	if filter.Region != "" {
		query["region"] = filter.Region
	}
	if filter.Country != "" {
		query["country"] = filter.Country
	}
	if filter.BestFor != "" {
		query["best_for"] = filter.BestFor
	}
	if filter.Month > 0 && filter.Month <= 12 {
		query["best_months"] = filter.Month
	}
	if filter.MinBudget > 0 {
		query["avg_daily_budget"] = bson.M{"$gte": filter.MinBudget}
	}
	if filter.MaxBudget > 0 {
		if existing, ok := query["avg_daily_budget"].(bson.M); ok {
			existing["$lte"] = filter.MaxBudget
		} else {
			query["avg_daily_budget"] = bson.M{"$lte": filter.MaxBudget}
		}
	}
	if filter.Featured != nil {
		query["featured"] = *filter.Featured
	}
	if filter.Search != "" {
		query["$or"] = []bson.M{
			{"name": bson.M{"$regex": filter.Search, "$options": "i"}},
			{"country": bson.M{"$regex": filter.Search, "$options": "i"}},
			{"description": bson.M{"$regex": filter.Search, "$options": "i"}},
			{"tags": bson.M{"$regex": filter.Search, "$options": "i"}},
		}
	}

	// Count total
	total, err := r.collection.CountDocuments(ctx, query)
	if err != nil {
		return nil, 0, err
	}

	// Sort options
	sortField := "popularity_score"
	sortOrder := -1
	switch filter.SortBy {
	case "name":
		sortField = "name"
		sortOrder = 1
	case "budget_asc":
		sortField = "avg_daily_budget"
		sortOrder = 1
	case "budget_desc":
		sortField = "avg_daily_budget"
		sortOrder = -1
	}

	opts := options.Find().
		SetSort(bson.D{{Key: sortField, Value: sortOrder}}).
		SetSkip(int64(filter.Offset))

	if filter.Limit > 0 {
		opts.SetLimit(int64(filter.Limit))
	}

	cursor, err := r.collection.Find(ctx, query, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var destinations []models.Destination
	if err := cursor.All(ctx, &destinations); err != nil {
		return nil, 0, err
	}

	return destinations, total, nil
}

func (r *DestinationRepository) FindFeatured(ctx context.Context, limit int) ([]models.Destination, error) {
	featured := true
	destinations, _, err := r.Find(ctx, models.DestinationFilter{
		Featured: &featured,
		Limit:    limit,
		SortBy:   "popularity",
	})
	return destinations, err
}

func (r *DestinationRepository) FindPopular(ctx context.Context, limit int) ([]models.Destination, error) {
	destinations, _, err := r.Find(ctx, models.DestinationFilter{
		Limit:  limit,
		SortBy: "popularity",
	})
	return destinations, err
}

func (r *DestinationRepository) FindByCategory(ctx context.Context, category string, limit int) ([]models.Destination, error) {
	destinations, _, err := r.Find(ctx, models.DestinationFilter{
		Category: category,
		Limit:    limit,
		SortBy:   "popularity",
	})
	return destinations, err
}

func (r *DestinationRepository) GetCategories(ctx context.Context) ([]string, error) {
	results, err := r.collection.Distinct(ctx, "categories", bson.M{"active": true})
	if err != nil {
		return nil, err
	}

	categories := make([]string, 0, len(results))
	for _, r := range results {
		if s, ok := r.(string); ok {
			categories = append(categories, s)
		}
	}
	return categories, nil
}

func (r *DestinationRepository) GetRegions(ctx context.Context) ([]string, error) {
	results, err := r.collection.Distinct(ctx, "region", bson.M{"active": true})
	if err != nil {
		return nil, err
	}

	regions := make([]string, 0, len(results))
	for _, r := range results {
		if s, ok := r.(string); ok {
			regions = append(regions, s)
		}
	}
	return regions, nil
}

func (r *DestinationRepository) Update(ctx context.Context, dest *models.Destination) error {
	dest.UpdatedAt = time.Now()
	_, err := r.collection.ReplaceOne(ctx, bson.M{"_id": dest.ID}, dest)
	return err
}

// Upsert inserts a destination if no document with the same name exists, otherwise does nothing.
func (r *DestinationRepository) Upsert(ctx context.Context, dest *models.Destination) error {
	now := time.Now()
	dest.Active = true

	filter := bson.M{"name": dest.Name}
	update := bson.M{
		"$setOnInsert": bson.M{
			"name":             dest.Name,
			"country":          dest.Country,
			"country_code":     dest.CountryCode,
			"region":           dest.Region,
			"description":      dest.Description,
			"image_url":        dest.ImageURL,
			"airport_code":     dest.AirportCode,
			"latitude":         dest.Latitude,
			"longitude":        dest.Longitude,
			"categories":       dest.Categories,
			"best_for":         dest.BestFor,
			"best_months":      dest.BestMonths,
			"avg_daily_budget": dest.AvgDailyBudget,
			"currency":         dest.Currency,
			"popularity_score": dest.PopularityScore,
			"featured":         dest.Featured,
			"active":           true,
			"created_at":       now,
			"updated_at":       now,
		},
	}

	opts := options.Update().SetUpsert(true)
	_, err := r.collection.UpdateOne(ctx, filter, update, opts)
	return err
}

func (r *DestinationRepository) IncrementListingsCount(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{
			"$inc": bson.M{"listings_count": 1},
			"$set": bson.M{"updated_at": time.Now()},
		},
	)
	return err
}
