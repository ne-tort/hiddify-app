package service

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseClientSaveGroupIDsField(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		json    string
		wantPr  bool
		wantIds []uint
		wantErr bool
	}{
		{"absent", `{"name":"a","enable":true}`, false, nil, false},
		{"empty array", `{"group_ids":[]}`, true, []uint{}, false},
		{"null", `{"group_ids":null}`, true, []uint{}, false},
		{"ids", `{"group_ids":[1,2]}`, true, []uint{1, 2}, false},
		{"invalid element", `{"group_ids":["x"]}`, true, nil, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pr, ids, err := parseClientSaveGroupIDsField(json.RawMessage(tc.json))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if pr != tc.wantPr {
				t.Fatalf("present: got %v want %v", pr, tc.wantPr)
			}
			if !reflect.DeepEqual(ids, tc.wantIds) {
				t.Fatalf("ids: got %#v want %#v", ids, tc.wantIds)
			}
		})
	}
}
