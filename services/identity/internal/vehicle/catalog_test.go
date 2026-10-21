package vehicle

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/unicode/norm"
)

// Names are matched exactly, so a stray space or a decomposed ë (an e and a combining
// diaeresis, which looks the same) would reject what a form sends back.
func TestCatalogIsWellFormed(t *testing.T) {
	assertNames := func(t *testing.T, names []string) {
		t.Helper()
		seen := map[string]bool{}
		for _, name := range names {
			assert.NotEmpty(t, name)
			assert.Equal(t, strings.TrimSpace(name), name, "untrimmed name %q", name)
			assert.True(t, norm.NFC.IsNormalString(name), "%q isn't in NFC", name)
			assert.False(t, seen[name], "%q appears twice", name)
			seen[name] = true
		}
	}

	var makeNames []string
	for _, m := range Makes {
		makeNames = append(makeNames, m.Name)
		assert.NotEmpty(t, m.Models, "%s has no models", m.Name)
		assertNames(t, m.Models)
	}
	assertNames(t, makeNames)
	assertNames(t, Colours)
}

func TestCheckCatalog(t *testing.T) {
	tests := []struct {
		name    string
		make    string
		model   string
		colour  string
		wantErr string
	}{
		{name: "in the catalog", make: "Toyota", model: "Corolla", colour: "White"},
		{name: "with a diaeresis", make: "Citroën", model: "C5 Aircross", colour: "Grey"},
		{name: "unknown make", make: "Trabant", model: "601", colour: "White", wantErr: "invalid vehicle: make isn't in the catalog"},
		{name: "make in another case", make: "toyota", model: "Corolla", colour: "White", wantErr: "invalid vehicle: make isn't in the catalog"},
		{name: "make without its diacritic", make: "Skoda", model: "Octavia", colour: "White", wantErr: "invalid vehicle: make isn't in the catalog"},
		{name: "another make's model", make: "Renault", model: "Corolla", colour: "White", wantErr: "invalid vehicle: model isn't one of the make's models"},
		{name: "unknown colour", make: "Toyota", model: "Corolla", colour: "Mauve", wantErr: "invalid vehicle: colour isn't in the catalog"},
		{name: "empty", wantErr: "invalid vehicle: make isn't in the catalog"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckCatalog(tt.make, tt.model, tt.colour)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrInvalid)
			assert.Equal(t, tt.wantErr, err.Error())
		})
	}
}
