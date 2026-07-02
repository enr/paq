package ui

import (
	"reflect"
	"testing"
)

func TestColWidths(t *testing.T) {
	cases := []struct {
		name    string
		headers []string
		rows    [][]string
		want    []int
	}{
		{
			name:    "header wider than all cells",
			headers: []string{"NAME", "VERSION"},
			rows:    [][]string{{"rg", "1.0"}},
			want:    []int{4, 7},
		},
		{
			name:    "cell wider than header",
			headers: []string{"NAME", "VERSION"},
			rows:    [][]string{{"a-very-long-tool-name", "1.0"}},
			want:    []int{21, 7},
		},
		{
			name:    "no rows",
			headers: []string{"NAME", "VERSION"},
			rows:    nil,
			want:    []int{4, 7},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := colWidths(tc.headers, tc.rows)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("colWidths(%v, %v) = %v, want %v", tc.headers, tc.rows, got, tc.want)
			}
		})
	}
}
