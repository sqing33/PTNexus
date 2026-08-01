package mapping

import "testing"

func TestResolveBasicPublishMappingsAudiencesKeepsHDR10Tag(t *testing.T) {
	mapped := ResolveBasicPublishMappings("audiences", map[string]any{
		"standardized_params": map[string]any{
			"tags": []any{"tag.HDR10"},
		},
	})

	if mapped["tags[0]"] != "hdr10" {
		t.Fatalf("expected audiences HDR10 tag value hdr10, got fields=%v", mapped)
	}
}
