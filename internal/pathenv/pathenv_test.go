package pathenv

import "testing"

func TestListContains(t *testing.T) {
	tests := []struct {
		name     string
		pathList string
		dir      string
		want     bool
	}{
		{"empty list", "", `C:\Users\enrico\AppData\Local\paq\bin`, false},
		{"exact match", `C:\Windows;C:\Users\enrico\AppData\Local\paq\bin`, `C:\Users\enrico\AppData\Local\paq\bin`, true},
		{"case-insensitive", `c:\users\enrico\appdata\local\paq\bin`, `C:\Users\Enrico\AppData\Local\PAQ\bin`, true},
		{"trailing backslash in entry", `C:\Users\enrico\AppData\Local\paq\bin\`, `C:\Users\enrico\AppData\Local\paq\bin`, true},
		{"quoted entry", `"C:\Users\enrico\AppData\Local\paq\bin"`, `C:\Users\enrico\AppData\Local\paq\bin`, true},
		{"substring is not a match", `C:\Users\enrico\AppData\Local\paq\bin2`, `C:\Users\enrico\AppData\Local\paq\bin`, false},
		{"entry with spaces around", ` C:\tools ;C:\other`, `C:\tools`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := listContains(tt.pathList, tt.dir); got != tt.want {
				t.Errorf("listContains(%q, %q) = %v, want %v", tt.pathList, tt.dir, got, tt.want)
			}
		})
	}
}
