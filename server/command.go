package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"
)

const fetchTimeout = 12 * time.Second

// providerIDPattern guards the :providerId path segment against path traversal
// and injection. OpenUsage plugin ids are kebab-case (see docs/plugins/schema.md).
var providerIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

type commandMode string

const (
	modeAll      commandMode = "all"
	modeProvider commandMode = "provider"
	modeHelp     commandMode = "help"
)

type openusageRequest struct {
	Mode     commandMode
	Provider string // set only when Mode == modeProvider
}

func buildAutocompleteTree() *model.AutocompleteData {
	root := model.NewAutocompleteData(slashTrigger, "[all|<provider>|help]", "Private OpenUsage usage cards.")
	root.AddCommand(model.NewAutocompleteData("all", "", "Usage cards for every enabled provider."))

	provider := model.NewAutocompleteData("provider", "<provider>", "Usage card for one provider id.")
	provider.AddTextArgument("Provider id (e.g. claude, codex)", "<provider>", providerIDPattern.String())
	root.AddCommand(provider)

	root.AddCommand(model.NewAutocompleteData("help", "", "Show the OpenUsage command surface."))
	return root
}

func buildOpenusageRequest(raw string) (openusageRequest, error) {
	args := commandArgs(raw)
	if len(args) == 0 {
		return openusageRequest{Mode: modeAll}, nil
	}
	switch strings.ToLower(args[0]) {
	case "all", "summary":
		if len(args) > 1 {
			return openusageRequest{}, fmt.Errorf("`all` does not accept extra arguments: %s", strings.Join(args[1:], " "))
		}
		return openusageRequest{Mode: modeAll}, nil
	case "help", "h":
		return openusageRequest{Mode: modeHelp}, nil
	case "provider", "p":
		if len(args) != 2 {
			return openusageRequest{}, errors.New("usage: `/openusage provider <id>`")
		}
		return providerRequest(args[1])
	default:
		// Bare provider id, e.g. `/openusage claude`.
		if len(args) > 1 {
			return openusageRequest{}, fmt.Errorf("unexpected arguments after %q: %s", args[0], strings.Join(args[1:], " "))
		}
		return providerRequest(args[0])
	}
}

func providerRequest(id string) (openusageRequest, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	if !providerIDPattern.MatchString(id) {
		return openusageRequest{}, fmt.Errorf("invalid provider id %q; use lowercase letters, digits, and hyphens", id)
	}
	return openusageRequest{Mode: modeProvider, Provider: id}, nil
}

func commandArgs(raw string) []string {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return nil
	}
	trigger := strings.TrimPrefix(fields[0], "/")
	if trigger == slashTrigger {
		return fields[1:]
	}
	return fields
}

func (p *Plugin) ExecuteCommand(_ *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	client := p.getClient()
	uc := p.getUsageClient()
	botID := p.getBotUserID()
	if client == nil || uc == nil || botID == "" {
		return ephemeral("OpenUsage plugin is not fully activated"), nil
	}

	ok, err := p.isOpenusageBotDM(args.ChannelId)
	if err != nil {
		return ephemeral(fmt.Sprintf("OpenUsage could not inspect this channel: %v", err)), nil
	}
	if !ok {
		return ephemeral("OpenUsage only responds in its bot direct message. Open a DM with OpenUsage and run `/openusage` there."), nil
	}

	req, err := buildOpenusageRequest(args.Command)
	if err != nil {
		return ephemeral(err.Error()), nil
	}

	if req.Mode == modeHelp {
		post := botPost(args.ChannelId, botID, renderHelp()...)
		if err := client.Post.CreatePost(post); err != nil {
			return ephemeral(fmt.Sprintf("create post failed: %v", err)), nil
		}
		return &model.CommandResponse{}, nil
	}

	loading := loadingPost(args.ChannelId, botID)
	_ = client.Post.CreatePost(loading)

	// Fetch off the slash-command path so a slow netbird round trip never blocks
	// the handler past Mattermost's response deadline.
	go p.finishCommand(client, uc, args.ChannelId, botID, loading.Id, req)
	return &model.CommandResponse{}, nil
}

func (p *Plugin) finishCommand(client *pluginapi.Client, uc *usageClient, channelID, botID, loadingPostID string, req openusageRequest) {
	attachments := fetchAndRender(uc, req)

	if loadingPostID != "" {
		_ = client.Post.DeletePost(loadingPostID)
	}

	post := botPost(channelID, botID, attachments...)
	if err := client.Post.CreatePost(post); err != nil {
		client.Log.Error("create openusage result post failed", "err", err)
		return
	}
	go p.clearBotMessagesExcept(channelID, botID, post.Id)
}

func fetchAndRender(uc *usageClient, req openusageRequest) []*model.SlackAttachment {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()

	switch req.Mode {
	case modeProvider:
		snap, err := uc.fetchProvider(ctx, req.Provider)
		if err != nil {
			return []*model.SlackAttachment{providerErrorAttachment(req.Provider, err)}
		}
		return renderSnapshots([]providerSnapshot{snap})
	case modeAll:
		fallthrough
	default:
		snaps, err := uc.fetchAll(ctx)
		if err != nil {
			return []*model.SlackAttachment{errorAttachment("OpenUsage — request failed", fmt.Sprintf("`%v`", err))}
		}
		return renderSnapshots(snaps)
	}
}

func providerErrorAttachment(providerID string, err error) *model.SlackAttachment {
	switch {
	case errors.Is(err, errProviderUnknown):
		return errorAttachment(
			"OpenUsage — unknown provider",
			fmt.Sprintf("`%s` is not a provider OpenUsage knows about. Run `/openusage all` to list providers with data.", providerID),
		)
	case errors.Is(err, errProviderNotCached):
		return errorAttachment(
			"OpenUsage — no data yet",
			fmt.Sprintf("`%s` is known but has no successful probe snapshot cached yet. Try again after the next refresh.", providerID),
		)
	case errors.Is(err, errServerBusy):
		return errorAttachment("OpenUsage — busy", "OpenUsage is handling too many connections right now. Retry shortly.")
	default:
		return errorAttachment("OpenUsage — request failed", fmt.Sprintf("`%v`", err))
	}
}

func loadingPost(channelID, botID string) *model.Post {
	post := &model.Post{ChannelId: channelID, UserId: botID}
	model.ParseSlackAttachment(post, []*model.SlackAttachment{{
		Text:  "Loading…",
		Color: colorAccent,
	}})
	return post
}

func botPost(channelID, botID string, attachments ...*model.SlackAttachment) *model.Post {
	post := &model.Post{ChannelId: channelID, UserId: botID}
	model.ParseSlackAttachment(post, attachments)
	return post
}

func (p *Plugin) isOpenusageBotDM(channelID string) (bool, error) {
	client := p.getClient()
	if client == nil {
		return false, errors.New("plugin API client is unavailable")
	}
	channel, err := client.Channel.Get(channelID)
	if err != nil {
		return false, err
	}
	return isOpenusageBotDM(channel, p.getBotUserID()), nil
}

func isOpenusageBotDM(channel *model.Channel, botID string) bool {
	if channel == nil || botID == "" {
		return false
	}
	return model.IsBotDMChannel(channel, botID)
}

func (p *Plugin) clearBotMessagesExcept(channelID, botID, keepPostID string) {
	client := p.getClient()
	if client == nil {
		return
	}
	var toDelete []string
	const perPage = 200
	for page := 0; ; page++ {
		postList, err := client.Post.GetPostsForChannel(channelID, page, perPage)
		if err != nil || postList == nil {
			break
		}
		for _, postID := range postList.Order {
			if postID == keepPostID {
				continue
			}
			if post := postList.Posts[postID]; post != nil && post.UserId == botID {
				toDelete = append(toDelete, post.Id)
			}
		}
		if len(postList.Order) < perPage {
			break
		}
	}
	for _, id := range toDelete {
		_ = client.Post.DeletePost(id)
	}
}

func ephemeral(text string) *model.CommandResponse {
	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         text,
	}
}
