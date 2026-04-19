package service

import (
	"testing"

	"github.com/realestayer/v4/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// These test the pure permission logic without touching Mongo. We can call
// the helper methods directly because they take a Trip by value.

func TestTripAccessAndEditPermissions(t *testing.T) {
	s := &TripService{} // method receiver only needs the value struct
	owner := primitive.NewObjectID()
	editor := primitive.NewObjectID()
	viewer := primitive.NewObjectID()
	legacyShare := primitive.NewObjectID()
	stranger := primitive.NewObjectID()

	trip := &models.Trip{
		UserID:     owner,
		SharedWith: []primitive.ObjectID{legacyShare},
		Collaborators: []models.TripCollaborator{
			{UserID: editor, Role: models.TripRoleEditor},
			{UserID: viewer, Role: models.TripRoleViewer},
		},
	}

	// Read access:
	if !s.hasAccess(owner.Hex(), trip) {
		t.Errorf("owner should read")
	}
	if !s.hasAccess(editor.Hex(), trip) {
		t.Errorf("editor should read")
	}
	if !s.hasAccess(viewer.Hex(), trip) {
		t.Errorf("viewer should read")
	}
	if !s.hasAccess(legacyShare.Hex(), trip) {
		t.Errorf("legacy shared_with user should still read")
	}
	if s.hasAccess(stranger.Hex(), trip) {
		t.Errorf("unrelated user should not read")
	}

	// Edit access:
	if !s.CanEdit(owner.Hex(), trip) {
		t.Errorf("owner should edit")
	}
	if !s.CanEdit(editor.Hex(), trip) {
		t.Errorf("editor should edit")
	}
	if s.CanEdit(viewer.Hex(), trip) {
		t.Errorf("viewer should NOT edit")
	}
	if s.CanEdit(legacyShare.Hex(), trip) {
		t.Errorf("legacy shared user should NOT edit (reader-only)")
	}
	if s.CanEdit(stranger.Hex(), trip) {
		t.Errorf("stranger should not edit")
	}
}

func TestTripAccessNilTrip(t *testing.T) {
	s := &TripService{}
	if s.hasAccess("anything", nil) {
		t.Errorf("nil trip → no access")
	}
	if s.CanEdit("anything", nil) {
		t.Errorf("nil trip → no edit")
	}
}
