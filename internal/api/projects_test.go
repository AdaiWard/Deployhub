package api

import "testing"

func TestParseGitHubRepository(t *testing.T) {
	tests := []struct {
		input    string
		fullName string
		cloneURL string
	}{
		{"octocat/Hello-World", "octocat/Hello-World", "https://github.com/octocat/Hello-World.git"},
		{"https://github.com/octocat/Hello-World", "octocat/Hello-World", "https://github.com/octocat/Hello-World.git"},
		{"https://github.com/octocat/Hello-World.git", "octocat/Hello-World", "https://github.com/octocat/Hello-World.git"},
	}
	for _, test := range tests {
		fullName, cloneURL, err := parseGitHubRepository(test.input)
		if err != nil {
			t.Fatalf("parse %q: %v", test.input, err)
		}
		if fullName != test.fullName || cloneURL != test.cloneURL {
			t.Fatalf("parse %q returned %q and %q", test.input, fullName, cloneURL)
		}
	}
}

func TestParseGitHubRepositoryRejectsOtherHosts(t *testing.T) {
	if _, _, err := parseGitHubRepository("https://example.com/owner/repo"); err == nil {
		t.Fatal("expected non GitHub repository to fail")
	}
}

func TestSlugify(t *testing.T) {
	if value := slugify("  My FastAPI_Project  "); value != "my-fastapi-project" {
		t.Fatalf("unexpected slug %q", value)
	}
}
