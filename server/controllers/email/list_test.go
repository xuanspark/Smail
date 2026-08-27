package email

import "testing"

func TestNormalizeEmailListRequest(t *testing.T) {
	tests := []struct {
		name       string
		request    emailRequest
		wantOffset int
		wantErr    bool
	}{
		{name: "defaults", request: emailRequest{}, wantOffset: 0},
		{name: "valid", request: emailRequest{CurrentPage: 3, PageSize: 20}, wantOffset: 40},
		{name: "negative page", request: emailRequest{CurrentPage: -1, PageSize: 20}, wantErr: true},
		{name: "large page", request: emailRequest{CurrentPage: maxEmailPage + 1, PageSize: 20}, wantErr: true},
		{name: "negative page size", request: emailRequest{CurrentPage: 1, PageSize: -1}, wantErr: true},
		{name: "large page size", request: emailRequest{CurrentPage: 1, PageSize: maxEmailPageSize + 1}, wantErr: true},
		{name: "large keyword", request: emailRequest{Keyword: string(make([]byte, maxEmailKeywordBytes+1))}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := tt.request
			offset, err := normalizeEmailListRequest(&request)
			if (err != nil) != tt.wantErr {
				t.Fatalf("normalizeEmailListRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && offset != tt.wantOffset {
				t.Fatalf("normalizeEmailListRequest() offset = %d, want %d", offset, tt.wantOffset)
			}
			if tt.name == "defaults" && (request.CurrentPage != 1 || request.PageSize != defaultEmailPageSize) {
				t.Fatalf("defaults were not applied: %+v", request)
			}
		})
	}
}

func TestDecodeStoredJSON(t *testing.T) {
	var users []User
	if err := decodeStoredJSON("  ", &users); err != nil {
		t.Fatalf("blank stored JSON was rejected: %v", err)
	}
	if err := decodeStoredJSON(`[{"Name":"Alice","EmailAddress":"alice@example.com"}]`, &users); err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].EmailAddress != "alice@example.com" {
		t.Fatalf("unexpected users: %#v", users)
	}
	if err := decodeStoredJSON("{", &users); err == nil {
		t.Fatal("corrupted stored JSON was accepted")
	}
}
