// Package vehicle holds the rules for a driver's vehicle: the catalog of makes, models
// and colours a driver picks from, and what a Portuguese plate looks like. It knows
// nothing about gRPC or the database.
package vehicle

import (
	"errors"
	"fmt"
	"slices"
)

// ErrInvalid is wrapped by the errors for vehicle details that break the rules. The full
// message is written for the user.
var ErrInvalid = errors.New("invalid vehicle")

type Make struct {
	Name   string
	Models []string
}

// The catalog, in the order a form lists it. Names are stored as written here, so
// renaming one leaves vehicles saved with the old name. Callers must not change these.
var (
	Makes = []Make{
		{Name: "BMW", Models: []string{"3 Series", "5 Series", "i4", "iX1"}},
		{Name: "BYD", Models: []string{"Atto 3", "Dolphin", "Seal"}},
		{Name: "Citroën", Models: []string{"C4", "C5 Aircross", "C5 X"}},
		{Name: "Dacia", Models: []string{"Jogger", "Logan", "Sandero", "Spring"}},
		{Name: "Ford", Models: []string{"Focus", "Kuga"}},
		{Name: "Honda", Models: []string{"Civic", "HR-V", "Jazz"}},
		{Name: "Hyundai", Models: []string{"Ioniq", "Ioniq 5", "Ioniq 6", "Kona", "Tucson"}},
		{Name: "Kia", Models: []string{"Ceed", "EV6", "Niro", "Sportage", "XCeed"}},
		{Name: "Mercedes-Benz", Models: []string{"A-Class", "C-Class", "E-Class", "EQA", "EQB"}},
		{Name: "MG", Models: []string{"MG4", "MG5", "ZS"}},
		{Name: "Nissan", Models: []string{"Leaf", "Qashqai"}},
		{Name: "Opel", Models: []string{"Astra", "Corsa"}},
		{Name: "Peugeot", Models: []string{"208", "308", "408", "508", "2008", "3008", "5008"}},
		{Name: "Renault", Models: []string{"Austral", "Captur", "Clio", "Mégane", "Zoe"}},
		{Name: "Seat", Models: []string{"Ibiza", "Leon"}},
		{Name: "Škoda", Models: []string{"Enyaq", "Fabia", "Octavia", "Scala", "Superb"}},
		{Name: "Tesla", Models: []string{"Model 3", "Model S", "Model Y"}},
		{Name: "Toyota", Models: []string{"C-HR", "Corolla", "Prius", "RAV4", "Yaris"}},
		{Name: "Volkswagen", Models: []string{"Golf", "ID.3", "ID.4", "Passat", "Polo"}},
	}
	Colours = []string{
		"Beige", "Black", "Blue", "Brown", "Green", "Grey", "Orange", "Red", "Silver", "White", "Yellow",
	}
)

// CheckCatalog returns an error unless the make, one of its models and the colour are in
// the catalog, spelt exactly as there.
func CheckCatalog(makeName, model, colour string) error {
	i := slices.IndexFunc(Makes, func(m Make) bool { return m.Name == makeName })
	if i < 0 {
		return fmt.Errorf("%w: make isn't in the catalog", ErrInvalid)
	}
	if !slices.Contains(Makes[i].Models, model) {
		return fmt.Errorf("%w: model isn't one of the make's models", ErrInvalid)
	}
	if !slices.Contains(Colours, colour) {
		return fmt.Errorf("%w: colour isn't in the catalog", ErrInvalid)
	}
	return nil
}
