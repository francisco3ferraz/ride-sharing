package vehicle

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanPlate(t *testing.T) {
	tests := []struct {
		name    string
		plate   string
		want    string
		wantErr bool
	}{
		{name: "since 2020", plate: "AB-12-CD", want: "AB-12-CD"},
		{name: "2005 to 2020", plate: "12-AB-34", want: "12-AB-34"},
		{name: "1992 to 2005", plate: "12-34-AB", want: "12-34-AB"},
		{name: "until 1992", plate: "AB-12-34", want: "AB-12-34"},
		{name: "lower case, no hyphens", plate: "ab12cd", want: "AB-12-CD"},
		{name: "spaces and hyphens anywhere", plate: " a-b 12--cd ", want: "AB-12-CD"},
		{name: "all letters", plate: "AB-CD-EF", wantErr: true},
		{name: "all digits", plate: "12-34-56", wantErr: true},
		{name: "a layout never used", plate: "AB-CD-12", wantErr: true},
		{name: "a pair of a letter and a digit", plate: "A1-23-BC", wantErr: true},
		{name: "too short", plate: "AB-12-C", wantErr: true},
		{name: "too long", plate: "AB-12-CDE", wantErr: true},
		{name: "empty", plate: "", wantErr: true},
		{name: "another separator", plate: "AB.12.CD", wantErr: true},
		{name: "accented letter", plate: "ÁB-12-CD", wantErr: true},
		{name: "letter that upper-cases to ASCII", plate: "ıı-12-34", wantErr: true},
		{name: "full-width digits", plate: "１２-34-AB", wantErr: true},
		{name: "NUL", plate: "AB-12-CD\x00", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CleanPlate(tt.plate)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalid)
				assert.Equal(t, "invalid vehicle: plate must look like AA-00-AA, 00-AA-00, 00-00-AA or AA-00-00", err.Error())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
