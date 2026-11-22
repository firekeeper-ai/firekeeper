package claudecloud

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPaginationRejectsInvalidCursors(t *testing.T) {
	tests := []struct {
		name    string
		cursors []string
		more    bool
		wantErr bool
	}{
		{"cycle", []string{"first", "second", "first"}, true, true},
		{"immediate repeat", []string{"first", "first"}, true, true},
		{"missing", []string{""}, true, true},
		{"too long", []string{strings.Repeat("x", maxCursorSize+1)}, true, true},
		{"last page", []string{""}, false, false},
	}
	for _, endpoint := range []string{"sessions", "events"} {
		for _, tc := range tests {
			t.Run(endpoint+"/"+tc.name, func(t *testing.T) {
				var calls atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					i := int(calls.Add(1)) - 1
					if i >= len(tc.cursors) {
						t.Error("pagination made an unexpected extra request")
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{
						"data":     []map[string]string{{"id": "session_test", "type": "user", "uuid": "event_test"}},
						"has_more": tc.more, "last_id": tc.cursors[i],
					})
				}))
				defer srv.Close()
				c := NewClient(Credentials{token: testToken, OrgUUID: testOrg})
				c.BaseURL = srv.URL
				var err error
				if endpoint == "sessions" {
					var got []SessionInfo
					got, err = c.Sessions(context.Background())
					if tc.wantErr && got != nil {
						t.Fatal("sessions returned partial results")
					}
				} else {
					var got []Event
					got, err = c.Events(context.Background(), sid)
					if tc.wantErr && got != nil {
						t.Fatal("events returned partial results")
					}
				}
				if (err != nil) != tc.wantErr {
					t.Fatalf("err = %v, want error %v", err, tc.wantErr)
				}
				if got := int(calls.Load()); got != len(tc.cursors) {
					t.Fatalf("requests = %d, want %d", got, len(tc.cursors))
				}
			})
		}
	}
}

func TestPageWalkLimits(t *testing.T) {
	t.Run("page count", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			n := calls.Add(1)
			_, _ = fmt.Fprintf(w, `{"data":[1],"has_more":true,"last_id":"cursor%d"}`, n)
		}))
		defer srv.Close()
		c := NewClient(Credentials{token: testToken, OrgUUID: testOrg})
		c.BaseURL = srv.URL
		err := walkPages(context.Background(), c, "/sessions", url.Values{}, 3, 1024, func(int) {})
		if err == nil || !strings.Contains(err.Error(), "too many") || calls.Load() != 3 {
			t.Fatalf("err = %v, requests = %d", err, calls.Load())
		}
	})
	t.Run("cumulative bytes", func(t *testing.T) {
		const first = `{"data":[1],"has_more":true,"last_id":"a"}`
		const last = `{"data":[2],"has_more":false,"last_id":"b"}`
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			if r.URL.Query().Get("after_id") == "" {
				_, _ = w.Write([]byte(first))
			} else {
				_, _ = w.Write([]byte(last))
			}
		}))
		defer srv.Close()
		c := NewClient(Credentials{token: testToken, OrgUUID: testOrg})
		c.BaseURL = srv.URL
		err := walkPages(context.Background(), c, "/sessions", url.Values{}, 3, int64(len(first)+len(last)-1), func(int) {})
		if err == nil || !strings.Contains(err.Error(), "paged response too large") || calls.Load() != 2 {
			t.Fatalf("err = %v, requests = %d", err, calls.Load())
		}
	})
}

func FuzzNextCursor(f *testing.F) {
	f.Add("", true)
	f.Add("cursor", true)
	f.Add(strings.Repeat("x", maxCursorSize+1), true)
	f.Add("last", false)
	f.Fuzz(func(t *testing.T, id string, hasMore bool) {
		seen := make(map[string]struct{})
		more, err := nextCursor(id, hasMore, seen)
		valid := hasMore && id != "" && len(id) <= maxCursorSize
		if more != valid || (err != nil) != (hasMore && !valid) {
			t.Fatal("cursor acceptance violated its bounds")
		}
		if valid {
			if _, ok := seen[id]; !ok {
				t.Fatal("accepted cursor was not remembered")
			}
			if more, err := nextCursor(id, true, seen); more || err == nil {
				t.Fatal("repeated cursor was accepted")
			}
		}
	})
}
