package main

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
	"github.com/mattermost/mattermost/server/public/pluginapi"
)

const (
	manifestID = "openusage"

	botUsername    = "openusage"
	botDisplayName = "OpenUsage"
	botDescription = "Private OpenUsage AI-subscription usage cards"

	slashTrigger = "openusage"

	httpClientTimeout = 15 * time.Second
)

type Plugin struct {
	plugin.MattermostPlugin

	mu        sync.RWMutex
	client    *pluginapi.Client
	usage     *usageClient
	baseURL   string
	botUserID string
	config    configuration
}

func (p *Plugin) OnActivate() error {
	client := pluginapi.NewClient(p.API, p.Driver)

	botID, err := client.Bot.EnsureBot(&model.Bot{
		Username:    botUsername,
		DisplayName: botDisplayName,
		Description: botDescription,
	})
	if err != nil {
		return fmt.Errorf("ensure bot: %w", err)
	}

	baseURL, err := resolveBaseURL()
	if err != nil {
		return fmt.Errorf("resolve OpenUsage base URL: %w", err)
	}
	uc := newUsageClient(baseURL, &http.Client{Timeout: httpClientTimeout})

	cmd := &model.Command{
		Trigger:          slashTrigger,
		AutoComplete:     true,
		AutoCompleteDesc: "Show private OpenUsage AI-subscription usage cards.",
		AutoCompleteHint: "[all|<provider>|help]",
		DisplayName:      botDisplayName,
		Description:      botDescription,
		AutocompleteData: buildAutocompleteTree(),
	}
	if err := client.SlashCommand.Register(cmd); err != nil {
		return fmt.Errorf("register slash /%s: %w", slashTrigger, err)
	}

	p.mu.Lock()
	p.client = client
	p.usage = uc
	p.baseURL = baseURL
	p.botUserID = botID
	p.mu.Unlock()

	if err := p.OnConfigurationChange(); err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	client.Log.Info("openusage plugin activated",
		"bot_user_id", botID,
		"base_url", baseURL,
	)
	return nil
}

func (p *Plugin) OnConfigurationChange() error {
	cfg, err := p.loadPluginConfiguration()
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.config = cfg
	p.mu.Unlock()
	return nil
}

func (p *Plugin) getClient() *pluginapi.Client {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.client
}

func (p *Plugin) getUsageClient() *usageClient {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.usage
}

func (p *Plugin) getBotUserID() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.botUserID
}
