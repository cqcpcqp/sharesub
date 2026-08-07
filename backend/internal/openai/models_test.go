package openai

import "testing"

func TestCodexModelsIncludeCurrentTextModels(t *testing.T) {
	want := map[string]bool{
		"gpt-5.6-sol":   false,
		"gpt-5.6":       false,
		"gpt-5.6-terra": false,
		"gpt-5.6-luna":  false,
		"gpt-5.5":       false,
		"gpt-image-1":   false,
		"gpt-image-1.5": false,
		"gpt-image-2":   false,
	}
	for _, model := range CodexModels {
		if _, ok := want[model.ID]; ok {
			want[model.ID] = true
		}
	}
	for model, found := range want {
		if !found {
			t.Errorf("model %q is missing", model)
		}
	}
}
