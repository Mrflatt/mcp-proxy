package proxy

import "testing"

func TestMatchesQuery(t *testing.T) {
	tests := []struct {
		name       string
		words      []string
		serverName string
		toolName   string
		want       bool
	}{
		// Single word matching server name includes all tools from that server.
		{
			name:       "server match only",
			words:      []string{"my-server"},
			serverName: "my-server",
			toolName:   "get_items",
			want:       true,
		},
		// Single word matching tool name.
		{
			name:       "tool match only",
			words:      []string{"item"},
			serverName: "my-server",
			toolName:   "get_items",
			want:       true,
		},
		// Single word matching neither.
		{
			name:       "no match",
			words:      []string{"other"},
			serverName: "my-server",
			toolName:   "get_items",
			want:       false,
		},
		// Server word satisfied globally; remaining words OR'd against tool name.
		{
			name:       "server + tool OR match first word",
			words:      []string{"my-server", "topology", "element"},
			serverName: "my-server",
			toolName:   "get_topology",
			want:       true,
		},
		{
			name:       "server + tool OR match second word",
			words:      []string{"my-server", "topology", "element"},
			serverName: "my-server",
			toolName:   "get_elements",
			want:       true,
		},
		{
			name:       "server match but tool matches none of remaining",
			words:      []string{"my-server", "topology", "element"},
			serverName: "my-server",
			toolName:   "get_items",
			want:       false,
		},
		// Hyphen and underscore treated as equivalent.
		{
			name:       "hyphen vs underscore in query",
			words:      []string{"managed-element"},
			serverName: "my-server",
			toolName:   "get_managed_element",
			want:       true,
		},
		{
			name:       "hyphen vs underscore in server name",
			words:      []string{"my-server"},
			serverName: "my-server",
			toolName:   "list_items",
			want:       true,
		},
		// All words match server name — include all tools.
		{
			name:       "all words match server",
			words:      []string{"my", "server"},
			serverName: "my-server",
			toolName:   "anything",
			want:       true,
		},
		// Empty words — no filter, always match.
		{
			name:       "empty words",
			words:      []string{},
			serverName: "my-server",
			toolName:   "get_items",
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesQuery(tt.words, tt.serverName, tt.toolName)
			if got != tt.want {
				t.Errorf("matchesQuery(%v, %q, %q) = %v, want %v",
					tt.words, tt.serverName, tt.toolName, got, tt.want)
			}
		})
	}
}
