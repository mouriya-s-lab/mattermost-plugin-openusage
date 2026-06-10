package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/mattermost/mattermost/server/public/pluginapi"
	"github.com/stretchr/testify/mock"
)

func TestBuildOpenusageRequest(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantMode commandMode
		wantProv string
		wantErr  bool
	}{
		{name: "empty defaults to all", raw: "/openusage", wantMode: modeAll},
		{name: "all", raw: "/openusage all", wantMode: modeAll},
		{name: "summary alias", raw: "/openusage summary", wantMode: modeAll},
		{name: "help", raw: "/openusage help", wantMode: modeHelp},
		{name: "bare provider", raw: "/openusage claude", wantMode: modeProvider, wantProv: "claude"},
		{name: "provider keyword", raw: "/openusage provider codex", wantMode: modeProvider, wantProv: "codex"},
		{name: "kebab provider", raw: "/openusage jetbrains-ai", wantMode: modeProvider, wantProv: "jetbrains-ai"},
		{name: "uppercased provider normalizes", raw: "/openusage CLAUDE", wantMode: modeProvider, wantProv: "claude"},
		{name: "all rejects extra args", raw: "/openusage all claude", wantErr: true},
		{name: "provider needs id", raw: "/openusage provider", wantErr: true},
		{name: "path traversal rejected", raw: "/openusage ../secrets", wantErr: true},
		{name: "slash in provider rejected", raw: "/openusage a/b", wantErr: true},
		{name: "extra args after provider", raw: "/openusage claude codex", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := buildOpenusageRequest(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got req %+v", req)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if req.Mode != tt.wantMode {
				t.Errorf("mode = %q, want %q", req.Mode, tt.wantMode)
			}
			if req.Provider != tt.wantProv {
				t.Errorf("provider = %q, want %q", req.Provider, tt.wantProv)
			}
		})
	}
}

func TestCommandArgsStripsTrigger(t *testing.T) {
	got := commandArgs("/openusage provider claude")
	want := []string{"provider", "claude"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestReusableBotPost(t *testing.T) {
	mkList := func(post *model.Post) *model.PostList {
		list := model.NewPostList()
		list.AddPost(post)
		list.AddOrder(post.Id)
		return list
	}

	t.Run("latest bot post is reused", func(t *testing.T) {
		api := &plugintest.API{}
		defer api.AssertExpectations(t)
		api.On("GetPostsForChannel", "ch", 0, 1).
			Return(mkList(&model.Post{Id: "p1", ChannelId: "ch", UserId: "bot"}), nil)

		p := &Plugin{client: pluginapi.NewClient(api, nil), botUserID: "bot"}
		got := p.reusableBotPost("ch", "bot")
		if got == nil || got.Id != "p1" {
			t.Fatalf("got %+v, want p1", got)
		}
	})

	t.Run("latest user post blocks reuse", func(t *testing.T) {
		api := &plugintest.API{}
		defer api.AssertExpectations(t)
		api.On("GetPostsForChannel", "ch", 0, 1).
			Return(mkList(&model.Post{Id: "p2", ChannelId: "ch", UserId: "human"}), nil)

		p := &Plugin{client: pluginapi.NewClient(api, nil), botUserID: "bot"}
		if got := p.reusableBotPost("ch", "bot"); got != nil {
			t.Fatalf("got %+v, want nil", got)
		}
	})
}

// TestFinishCommandEditsInPlace asserts the result lands as an edit of the
// loading post — no DeletePost, no extra CreatePost — so the webapp never
// shows a "(message deleted)" placeholder for the delivery itself.
func TestFinishCommandEditsInPlace(t *testing.T) {
	uc := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"providerId":"claude","displayName":"Claude","plan":"Pro","lines":[],"fetchedAt":"2026-03-26T08:15:30Z"}]`))
	})

	// Each mock returns a fresh instance: pluginapi shallow-copies the API's
	// return value back onto its argument, which deadlocks on a shared *Post.
	mkPost := func() *model.Post { return &model.Post{Id: "p1", ChannelId: "ch", UserId: "bot"} }
	api := &plugintest.API{}
	api.On("GetPost", "p1").Return(mkPost(), nil)
	api.On("UpdatePost", mock.MatchedBy(func(post *model.Post) bool { return post.Id == "p1" })).
		Return(mkPost(), nil).Once()
	// Steady-state cleanup goroutine: only the kept post exists, nothing to delete.
	keptOnly := model.NewPostList()
	keptOnly.AddPost(mkPost())
	keptOnly.AddOrder("p1")
	api.On("GetPostsForChannel", "ch", 0, 200).Return(keptOnly, nil).Maybe()

	p := &Plugin{client: pluginapi.NewClient(api, nil), usage: uc, botUserID: "bot"}
	p.finishCommand(p.getClient(), uc, "ch", "bot", "p1", openusageRequest{Mode: modeAll})

	time.Sleep(50 * time.Millisecond) // let the cleanup goroutine run against the Maybe mocks
	api.AssertNotCalled(t, "DeletePost", mock.Anything)
	api.AssertNotCalled(t, "CreatePost", mock.Anything)
	api.AssertExpectations(t)
}
