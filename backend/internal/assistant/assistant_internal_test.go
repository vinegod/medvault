package assistant

import "testing"

func TestNormalizeDate(t *testing.T) {
	tests := []struct{ in, want string }{
		{"2027-05-14", "2027-05-14"},
		{"05/2027", "2027-05-31"},
		{"2.2028", "2028-02-29"},
		{"2027-11", "2027-11-30"},
		{"13/2027", ""},
		{"soon", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeDate(tt.in); got != tt.want {
			t.Errorf("normalizeDate(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeUnit(t *testing.T) {
	tests := []struct{ in, want string }{
		{"tablets", "tablets"},
		{" Tablet ", "tablets"},
		{"capsuless", "capsules"},
		{"Patch", "patches"},
		{"ml", "ml"},
		{"Boxes", "boxes"},
	}
	for _, tt := range tests {
		if got := normalizeUnit(tt.in); got != tt.want {
			t.Errorf("normalizeUnit(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSplitWarning(t *testing.T) {
	tests := []struct{ in, text, warning string }{
		{"Take Aspirin.", "Take Aspirin.", ""},
		{"WARNING: see a doctor\nTake Aspirin.", "Take Aspirin.", "see a doctor"},
		{"**Warning:** high fever\n\nUse Nurofen.", "Use Nurofen.", "high fever"},
		{"Note: shake well", "Note: shake well", ""},
	}
	for _, tt := range tests {
		text, warning := splitWarning(tt.in)
		if text != tt.text || warning != tt.warning {
			t.Errorf("splitWarning(%q) = %q, %q; want %q, %q", tt.in, text, warning, tt.text, tt.warning)
		}
	}
}

func TestIsLocal(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"", false},
		{"http://localhost:11434/v1", true},
		{"http://192.168.1.10:1234/v1", true},
		{"http://ollama:11434/v1", true},
		{"https://box.tail1234.ts.net/v1", true},
		{"https://openrouter.ai/api/v1", false},
	}
	for _, tt := range tests {
		if got := isLocal(tt.url); got != tt.want {
			t.Errorf("isLocal(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}
