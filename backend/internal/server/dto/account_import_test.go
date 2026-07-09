package dto

import (
	"encoding/json"
	"testing"
)

func TestImportAccountsReqAcceptsLegacyArrayAndVersionedFiles(t *testing.T) {
	tests := []struct {
		name        string
		payload     string
		wantVersion int
		wantEmail   string
	}{
		{
			name:        "legacy array",
			payload:     `[{"name":"legacy-array","platform":"openai","credentials":{"email":"array@example.com"}}]`,
			wantVersion: 0,
			wantEmail:   "array@example.com",
		},
		{
			name:        "legacy envelope",
			payload:     `{"accounts":[{"name":"legacy-envelope","platform":"openai","credentials":{"email":"envelope@example.com"}}]}`,
			wantVersion: 0,
			wantEmail:   "envelope@example.com",
		},
		{
			name:        "version one",
			payload:     `{"version":1,"accounts":[{"name":"legacy-v1","platform":"openai","credentials":{"email":"v1@example.com"}}]}`,
			wantVersion: 1,
			wantEmail:   "v1@example.com",
		},
		{
			name:        "version two",
			payload:     `{"version":2,"accounts":[{"name":"current","email":"v2@example.com","platform":"openai","credentials":{"email":"v2@example.com"}}]}`,
			wantVersion: 2,
			wantEmail:   "v2@example.com",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var req ImportAccountsReq
			if err := json.Unmarshal([]byte(test.payload), &req); err != nil {
				t.Fatalf("Unmarshal returned error: %v", err)
			}
			if req.Version != test.wantVersion || len(req.Accounts) != 1 || req.Accounts[0].Credentials["email"] != test.wantEmail {
				t.Fatalf("request = %+v", req)
			}
		})
	}
}
