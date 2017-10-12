package metrics

import (
	"expvar"
	"net/http"
	"runtime"
	"time"

	"github.com/Seklfreak/Robyul2/cache"
	"github.com/Seklfreak/Robyul2/helpers"
	"github.com/Seklfreak/Robyul2/models"
	"github.com/bwmarrin/discordgo"
	rethink "github.com/gorethink/gorethink"
)

var (
	// MessagesReceived counts all ever received messages
	MessagesReceived = expvar.NewInt("messages_received")

	// UserCount counts all logged-in users
	UserCount = expvar.NewInt("user_count")

	// ChannelCount counts all watching channels
	ChannelCount = expvar.NewInt("channel_count")

	// GuildCount counts all joined guilds
	GuildCount = expvar.NewInt("guild_count")

	// CommandsExecuted increases after each command execution
	CommandsExecuted = expvar.NewInt("commands_executed")

	// PollsCreated increases everytime a new pool is created
	PollsCreated = expvar.NewInt("polls_created")

	// CleverbotRequests increases after each request to cleverbot.com
	CleverbotRequests = expvar.NewInt("cleverbot_requests")

	// CoroutineCount counts all running coroutines
	CoroutineCount = expvar.NewInt("coroutine_count")

	// Uptime stores the timestamp of the bot's boot
	Uptime = expvar.NewInt("uptime")

	// VliveChannelsCount counts all connected vlive channels
	VliveChannelsCount = expvar.NewInt("vlive_channels_count")

	// TwitterAccountsCount counts all connected twitter accounts
	TwitterAccountsCount = expvar.NewInt("twitter_accounts_count")

	// InstagramAccountsCount counts all connected instagram accounts
	InstagramAccountsCount = expvar.NewInt("instagram_accounts_count")

	// FacebookPagesCount counts all connected instagram accounts
	FacebookPagesCount = expvar.NewInt("facebook_pages_count")

	// RedditSubredditsCount counts all connected subreddits accounts
	RedditSubredditsCount = expvar.NewInt("reddit_subreddits_count")

	// WolframAlphaRequests increases after each request to wolframalpha.com
	WolframAlphaRequests = expvar.NewInt("wolframalpha_requests")

	// LastFmRequests increases after each request to last.fm
	LastFmRequests = expvar.NewInt("lastfm_requests")

	// DarkSkyRequests increases after each request to darksky.net
	DarkSkyRequests = expvar.NewInt("darksky_requests")

	// KeywordNotificationsSentCount increased after every keyword notification sent
	KeywordNotificationsSentCount = expvar.NewInt("keywordnotifications_sent_count")

	// GalleriesCount counts all galleries in the db
	GalleriesCount = expvar.NewInt("galleries_count")

	// GalleryPostsSent increased with every link reposted
	GalleryPostsSent = expvar.NewInt("gallery_posts_sent")

	// GalleriesCount counts all galleries in the db
	MirrorsCount = expvar.NewInt("mirrors_count")

	// GalleryPostsSent increased with every link reposted
	MirrorsPostsSent = expvar.NewInt("mirrors_posts_sent")

	// LevelImagesGeneratedCount increased with every level image generated
	LevelImagesGenerated = expvar.NewInt("levels_images_generated")

	// RandomPictureSourcesCount counts all randompicture sources connected
	RandomPictureSourcesCount = expvar.NewInt("randompictures_sources_count")

	// RandomPictureSourcesImagesCachedCount counts all randompicture images in cache
	RandomPictureSourcesImagesCachedCount = expvar.NewInt("randompictures_sources_imagescached_count")

	// CustomCommandsCount counts all custom commands
	CustomCommandsCount = expvar.NewInt("customcommands_count")

	// CustomCommandsTriggered increased with every time a custom command is triggered
	CustomCommandsTriggered = expvar.NewInt("customcommands_triggered")

	// ReactionPollsCount increased with every time a new ReactionPoll is created
	ReactionPollsCount = expvar.NewInt("reactionpolls_count")

	// MachineryDelayedTasksCount counts all delayed machinery tasks
	MachineryDelayedTasksCount = expvar.NewInt("machinery_delayedtasks_count")

	// YoutubeChannelCount counts all connected youtube channels
	YoutubeChannelsCount = expvar.NewInt("youtube_channel_count")

	// YoutubeLeftQuota counts how many left youtube quotas
	YoutubeLeftQuota = expvar.NewInt("youtube_left_quota")
)

// Init starts a http server on 127.0.0.1:1337
func Init() {
	cache.GetLogger().WithField("module", "metrics").Info("Listening on TCP/1337")
	Uptime.Set(time.Now().Unix())
	go http.ListenAndServe(helpers.GetConfig().Path("metrics_ip").Data().(string)+":1337", nil)
}

// OnReady listens for said discord event
func OnReady(session *discordgo.Session, event *discordgo.Ready) {
	go CollectDiscordMetrics(session)
	go CollectRuntimeMetrics()
}

// OnMessageCreate listens for said discord event
func OnMessageCreate(session *discordgo.Session, event *discordgo.MessageCreate) {
	MessagesReceived.Add(1)
}

// CollectDiscordMetrics counts Guilds, Channels and Users
func CollectDiscordMetrics(session *discordgo.Session) {
	for {
		time.Sleep(15 * time.Second)

		users := make(map[string]string)
		channels := 0
		guilds := session.State.Guilds

		for _, guild := range guilds {
			channels += len(guild.Channels)
			for _, u := range guild.Members {
				users[u.User.ID] = u.User.Username
			}
		}

		UserCount.Set(int64(len(users)))
		ChannelCount.Set(int64(channels))
		GuildCount.Set(int64(len(guilds)))
	}
}

// CollectRuntimeMetrics counts all running coroutines
func CollectRuntimeMetrics() {
	defer helpers.Recover()

	for {
		time.Sleep(15 * time.Second)

		CoroutineCount.Set(int64(runtime.NumGoroutine()))

		VliveChannelsCount.Set(entriesCount("vlive"))

		InstagramAccountsCount.Set(entriesCount("instagram"))

		TwitterAccountsCount.Set(entriesCount("twitter"))

		FacebookPagesCount.Set(entriesCount("facebook"))

		GalleriesCount.Set(entriesCount("galleries"))

		MirrorsCount.Set(entriesCount("mirrors"))

		RandomPictureSourcesCount.Set(entriesCount("randompictures_sources"))

		RedditSubredditsCount.Set(entriesCount(models.RedditSubredditsTable))

		YoutubeChannelsCount.Set(entriesCount(models.YoutubeChannelTable))

		key := "delayed_tasks"
		delayedTasks, err := cache.GetMachineryRedisClient().ZCard(key).Result()
		helpers.Relax(err)
		MachineryDelayedTasksCount.Set(delayedTasks)

		key = models.YoutubeQuotaRedisKey
		codec := cache.GetRedisCacheCodec()
		var q models.YoutubeQuota
		if err := codec.Get(key, &q); err == nil {
			YoutubeLeftQuota.Set(q.Left)
		} else {
			YoutubeLeftQuota.Set(0)
		}
	}
}

func entriesCount(table string) (cnt int64) {
	cursor, err := rethink.Table(table).Count().Run(helpers.GetDB())
	helpers.Relax(err)
	cursor.One(&cnt)
	cursor.Close()
	return
}
