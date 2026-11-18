package domain

import (
	"errors"
	"strings"
	"testing"

	"bakehouse/pkg/apperr"
)

func strp(s string) *string { return &s }

func TestFlavourFromInput(t *testing.T) {
	f, err := flavourFromInput(FlavourInput{Name: "  Indian-Inspired ", Description: strp("  "), BackgroundColour: strp("#f8e8e8")})
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "Indian-Inspired" || f.Slug != "indian-inspired" || f.Status != "active" {
		t.Fatalf("tidied: %+v", f)
	}
	if f.Description != nil {
		t.Fatal("a blank description is stored as none")
	}
	if *f.BackgroundColour != "#F8E8E8" {
		t.Fatalf("colour upper-cased: %s", *f.BackgroundColour)
	}
	for _, c := range []struct {
		in    FlavourInput
		field string
	}{
		{FlavourInput{}, "name"},
		{FlavourInput{Name: "!!!"}, "name"},
		{FlavourInput{Name: strings.Repeat("a", 61)}, "name"},
		{FlavourInput{Name: "Fruit", Description: strp(strings.Repeat("a", 201))}, "description"},
		{FlavourInput{Name: "Fruit", BackgroundColour: strp("pink")}, "backgroundColour"},
		{FlavourInput{Name: "Fruit", SortOrder: -1}, "sortOrder"},
		{FlavourInput{Name: "Fruit", Status: "hidden"}, "status"},
	} {
		_, err := flavourFromInput(c.in)
		var ae *apperr.Error
		if !errors.As(err, &ae) || len(ae.Fields) == 0 || ae.Fields[0].Field != c.field {
			t.Fatalf("%+v: want a %s error, got %v", c.in, c.field, err)
		}
	}
}
