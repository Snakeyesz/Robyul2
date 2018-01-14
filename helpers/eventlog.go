package helpers

import (
	"time"

	"sync"

	"strconv"

	"fmt"

	"strings"

	"github.com/Seklfreak/Robyul2/cache"
	"github.com/Seklfreak/Robyul2/models"
	"github.com/bwmarrin/discordgo"
	"github.com/pkg/errors"
)

var (
	AuditLogBackfillRequestsLock = sync.Mutex{}
)

func EventlogLog(createdAt time.Time, guildID, targetID, targetType, userID, actionType, reason string,
	changes []models.ElasticEventlogChange, options []models.ElasticEventlogOption, waitingForAuditLogBackfill bool) (added bool, err error) {
	if guildID == "" {
		return false, nil
	}

	if IsBlacklistedGuild(guildID) {
		return false, nil
	}

	if IsLimitedGuild(guildID) {
		return false, nil
	}

	if GuildSettingsGetCached(guildID).EventlogDisabled {
		return false, nil
	}

	if changes == nil {
		changes = make([]models.ElasticEventlogChange, 0)
	}

	if options == nil {
		options = make([]models.ElasticEventlogOption, 0)
	}

	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	// TODO: remove me
	cache.GetLogger().WithField("module", "helpers/eventlog").Debugf(
		"adding to eventlog time %s guildID %s targetID %s userID %s actionType %s reason %s changes %+v options %+v",
		createdAt.Format(time.RFC3339), guildID, targetID, userID, actionType, reason, changes, options,
	)

	messageIDs := make([]string, 0)
	eventlogChannelIDs := GuildSettingsGetCached(guildID).EventlogChannelIDs
	for _, eventlogChannelID := range eventlogChannelIDs {
		messages, _ := SendEmbed(eventlogChannelID, getEventlogEmbed(createdAt, guildID, targetID, targetType, userID, actionType, reason, changes, options, waitingForAuditLogBackfill))
		if messages != nil && len(messages) >= 1 {
			messageIDs = append(messageIDs, eventlogChannelID+"|"+messages[0].ID)
		}
	}

	err = ElasticAddEventlog(createdAt, guildID, targetID, targetType, userID, actionType, reason, changes, options, waitingForAuditLogBackfill, messageIDs)

	if err != nil {
		return false, err
	}

	return true, nil
}

func EventlogLogUpdate(elasticID string, UserID string,
	options []models.ElasticEventlogOption, changes []models.ElasticEventlogChange,
	reason string, auditLogBackfilled bool) (err error) {
	eventlogItem, err := ElasticUpdateEventLog(elasticID, UserID, options, changes, reason, auditLogBackfilled)
	if err != nil {
		return
	}

	if eventlogItem != nil && eventlogItem.EventlogMessages != nil && len(eventlogItem.EventlogMessages) > 0 {
		embed := getEventlogEmbed(eventlogItem.CreatedAt, eventlogItem.GuildID, eventlogItem.TargetID,
			eventlogItem.TargetType, eventlogItem.UserID, eventlogItem.ActionType, eventlogItem.Reason,
			eventlogItem.Changes, eventlogItem.Options, eventlogItem.WaitingFor.AuditLogBackfill)
		for _, messageID := range eventlogItem.EventlogMessages {
			if strings.Contains(messageID, "|") {
				parts := strings.SplitN(messageID, "|", 2)
				if len(parts) >= 2 {
					EditEmbed(parts[0], parts[1], embed)
				}
			}
		}
	}

	return
}

func getEventlogEmbed(createdAt time.Time, guildID, targetID, targetType, userID, actionType, reason string,
	changes []models.ElasticEventlogChange, options []models.ElasticEventlogOption, waitingForAuditLogBackfill bool) (embed *discordgo.MessageEmbed) {
	embed = &discordgo.MessageEmbed{
		URL:       "",
		Type:      "",
		Title:     actionType + ": #" + targetID + " (" + targetType + ")",
		Timestamp: createdAt.Format(time.RFC3339),
		Color:     0,
		Fields: []*discordgo.MessageEmbedField{{
			Name:  "Reason",
			Value: reason,
		},
		},
	}

	if changes != nil {
		for _, change := range changes {
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
				Name:  change.Key,
				Value: change.OldValue + " ➡ " + change.NewValue,
			})
		}
	}

	if options != nil {
		for _, option := range options {
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
				Name:  option.Key,
				Value: option.Value,
			})
		}
	}

	if userID != "" {
		user, err := GetUserWithoutAPI(userID)
		if err != nil {
			user = new(discordgo.User)
			user.Username = "N/A"
		}
		embed.Author = &discordgo.MessageEmbedAuthor{
			Name:    user.Username,
			IconURL: user.AvatarURL("64"),
		}
	}

	return embed
}

type AuditLogBackfillType int

const (
	AuditLogBackfillTypeChannelCreate AuditLogBackfillType = 1 << iota
	AuditLogBackfillTypeChannelDelete
	AuditLogBackfillTypeChannelUpdate
	AuditLogBackfillTypeRoleCreate
	AuditLogBackfillTypeRoleDelete
	AuditLogBackfillTypeBanAdd
	AuditLogBackfillTypeBanRemove
	AuditLogBackfillTypeMemberRemove
	AuditLogBackfillTypeEmojiCreate
	AuditLogBackfillTypeEmojiDelete
	AuditLogBackfillTypeEmojiUpdate
	AuditLogBackfillTypeGuildUpdate
	AuditLogBackfillTypeRoleUpdate
)

func RequestAuditLogBackfill(guildID string, backfillType AuditLogBackfillType) (err error) {
	AuditLogBackfillRequestsLock.Lock()
	defer AuditLogBackfillRequestsLock.Unlock()

	redis := cache.GetRedisClient()

	switch backfillType {
	case AuditLogBackfillTypeChannelCreate:
		cache.GetLogger().Infof("requested backfill for %s: %s", guildID, "channel create")
		_, err := redis.SAdd(models.AuditLogBackfillTypeChannelCreateRedisSet, guildID).Result()
		return err
	case AuditLogBackfillTypeChannelDelete:
		cache.GetLogger().Infof("requested backfill for %s: %s", guildID, "channel delete")
		_, err := redis.SAdd(models.AuditLogBackfillTypeChannelDeleteRedisSet, guildID).Result()
		return err
	case AuditLogBackfillTypeChannelUpdate:
		cache.GetLogger().Infof("requested backfill for %s: %s", guildID, "channel update")
		_, err := redis.SAdd(models.AuditLogBackfillTypeChannelUpdateRedisSet, guildID).Result()
		return err
	case AuditLogBackfillTypeRoleCreate:
		cache.GetLogger().Infof("requested backfill for %s: %s", guildID, "role create")
		_, err := redis.SAdd(models.AuditLogBackfillTypeRoleCreateRedisSet, guildID).Result()
		return err
	case AuditLogBackfillTypeRoleDelete:
		cache.GetLogger().Infof("requested backfill for %s: %s", guildID, "role delete")
		_, err := redis.SAdd(models.AuditLogBackfillTypeRoleDeleteRedisSet, guildID).Result()
		return err
	case AuditLogBackfillTypeBanAdd:
		cache.GetLogger().Infof("requested backfill for %s: %s", guildID, "ban add")
		_, err := redis.SAdd(models.AuditLogBackfillTypeBanAddRedisSet, guildID).Result()
		return err
	case AuditLogBackfillTypeBanRemove:
		cache.GetLogger().Infof("requested backfill for %s: %s", guildID, "ban remove")
		_, err := redis.SAdd(models.AuditLogBackfillTypeBanRemoveRedisSet, guildID).Result()
		return err
	case AuditLogBackfillTypeMemberRemove:
		cache.GetLogger().Infof("requested backfill for %s: %s", guildID, "member remove")
		_, err := redis.SAdd(models.AuditLogBackfillTypeMemberRemoveRedisSet, guildID).Result()
		return err
	case AuditLogBackfillTypeEmojiCreate:
		cache.GetLogger().Infof("requested backfill for %s: %s", guildID, "emoji create")
		_, err := redis.SAdd(models.AuditLogBackfillTypeEmojiCreateRedisSet, guildID).Result()
		return err
	case AuditLogBackfillTypeEmojiDelete:
		cache.GetLogger().Infof("requested backfill for %s: %s", guildID, "emoji delete")
		_, err := redis.SAdd(models.AuditLogBackfillTypeEmojiDeleteRedisSet, guildID).Result()
		return err
	case AuditLogBackfillTypeEmojiUpdate:
		cache.GetLogger().Infof("requested backfill for %s: %s", guildID, "emoji update")
		_, err := redis.SAdd(models.AuditLogBackfillTypeEmojiUpdateRedisSet, guildID).Result()
		return err
	case AuditLogBackfillTypeGuildUpdate:
		cache.GetLogger().Infof("requested backfill for %s: %s", guildID, "guild update")
		_, err := redis.SAdd(models.AuditLogBackfillTypeGuildUpdateRedisSet, guildID).Result()
		return err
	case AuditLogBackfillTypeRoleUpdate:
		cache.GetLogger().Infof("requested backfill for %s: %s", guildID, "role update")
		_, err := redis.SAdd(models.AuditLogBackfillTypeRoleUpdateRedisSet, guildID).Result()
		return err
	}
	return errors.New("unknown backfill type")
}

func OnEventlogEmojiCreate(guildID string, emoji *discordgo.Emoji) {
	leftAt := time.Now()

	options := make([]models.ElasticEventlogOption, 0)

	options = append(options, models.ElasticEventlogOption{
		Key:   "emoji_name",
		Value: emoji.Name,
	})

	options = append(options, models.ElasticEventlogOption{
		Key:   "emoji_managed",
		Value: StoreBoolAsString(emoji.Managed),
	})

	options = append(options, models.ElasticEventlogOption{
		Key:   "emoji_requirecolons",
		Value: StoreBoolAsString(emoji.RequireColons),
	})

	options = append(options, models.ElasticEventlogOption{
		Key:   "emoji_animated",
		Value: StoreBoolAsString(emoji.Animated),
	})

	options = append(options, models.ElasticEventlogOption{
		Key:   "emoji_apiname",
		Value: emoji.APIName(),
	})

	added, err := EventlogLog(leftAt, guildID, emoji.ID, models.EventlogTargetTypeEmoji, "", models.EventlogTypeEmojiCreate, "", nil, options, true)
	RelaxLog(err)
	if added {
		err := RequestAuditLogBackfill(guildID, AuditLogBackfillTypeEmojiCreate)
		RelaxLog(err)
	}
}

func OnEventlogEmojiDelete(guildID string, emoji *discordgo.Emoji) {
	leftAt := time.Now()

	options := make([]models.ElasticEventlogOption, 0)

	options = append(options, models.ElasticEventlogOption{
		Key:   "emoji_name",
		Value: emoji.Name,
	})

	options = append(options, models.ElasticEventlogOption{
		Key:   "emoji_managed",
		Value: StoreBoolAsString(emoji.Managed),
	})

	options = append(options, models.ElasticEventlogOption{
		Key:   "emoji_requirecolons",
		Value: StoreBoolAsString(emoji.RequireColons),
	})

	options = append(options, models.ElasticEventlogOption{
		Key:   "emoji_animated",
		Value: StoreBoolAsString(emoji.Animated),
	})

	options = append(options, models.ElasticEventlogOption{
		Key:   "emoji_apiname",
		Value: emoji.APIName(),
	})

	added, err := EventlogLog(leftAt, guildID, emoji.ID, models.EventlogTargetTypeEmoji, "", models.EventlogTypeEmojiDelete, "", nil, options, true)
	RelaxLog(err)
	if added {
		err := RequestAuditLogBackfill(guildID, AuditLogBackfillTypeEmojiDelete)
		RelaxLog(err)
	}
}

func OnEventlogEmojiUpdate(guildID string, oldEmoji, newEmoji *discordgo.Emoji) {
	leftAt := time.Now()

	options := make([]models.ElasticEventlogOption, 0)

	options = append(options, models.ElasticEventlogOption{
		Key:   "emoji_name",
		Value: newEmoji.Name,
	})

	options = append(options, models.ElasticEventlogOption{
		Key:   "emoji_managed",
		Value: StoreBoolAsString(newEmoji.Managed),
	})

	options = append(options, models.ElasticEventlogOption{
		Key:   "emoji_requirecolons",
		Value: StoreBoolAsString(newEmoji.RequireColons),
	})

	options = append(options, models.ElasticEventlogOption{
		Key:   "emoji_animated",
		Value: StoreBoolAsString(newEmoji.Animated),
	})

	options = append(options, models.ElasticEventlogOption{
		Key:   "emoji_apiname",
		Value: newEmoji.APIName(),
	})

	changes := make([]models.ElasticEventlogChange, 0)

	if oldEmoji.Name != newEmoji.Name {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "emoji_name",
			OldValue: oldEmoji.Name,
			NewValue: newEmoji.Name,
		})
	}

	if oldEmoji.Managed != newEmoji.Managed {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "emoji_managed",
			OldValue: StoreBoolAsString(oldEmoji.Managed),
			NewValue: StoreBoolAsString(newEmoji.Managed),
		})
	}

	if oldEmoji.RequireColons != newEmoji.RequireColons {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "emoji_requirecolons",
			OldValue: StoreBoolAsString(oldEmoji.RequireColons),
			NewValue: StoreBoolAsString(newEmoji.RequireColons),
		})
	}

	if oldEmoji.Animated != newEmoji.Animated {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "emoji_animated",
			OldValue: StoreBoolAsString(oldEmoji.Animated),
			NewValue: StoreBoolAsString(newEmoji.Animated),
		})
	}

	if oldEmoji.APIName() != newEmoji.APIName() {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "emoji_apiname",
			OldValue: oldEmoji.APIName(),
			NewValue: newEmoji.APIName(),
		})
	}

	added, err := EventlogLog(leftAt, guildID, newEmoji.ID, models.EventlogTargetTypeEmoji, "", models.EventlogTypeEmojiUpdate, "", changes, options, true)
	RelaxLog(err)
	if added {
		err := RequestAuditLogBackfill(guildID, AuditLogBackfillTypeEmojiUpdate)
		RelaxLog(err)
	}
}

func OnEventlogGuildUpdate(guildID string, oldGuild, newGuild *discordgo.Guild) {
	leftAt := time.Now()

	changes := make([]models.ElasticEventlogChange, 0)
	if oldGuild.Name != newGuild.Name {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "guild_name",
			OldValue: oldGuild.Name,
			NewValue: newGuild.Name,
		})
	}

	if oldGuild.Icon != newGuild.Icon {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "guild_icon",
			OldValue: oldGuild.Icon,
			NewValue: newGuild.Icon,
		})
	}

	if oldGuild.Region != newGuild.Region {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "guild_region",
			OldValue: oldGuild.Region,
			NewValue: newGuild.Region,
		})
	}

	if oldGuild.AfkChannelID != newGuild.AfkChannelID {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "guild_afkchannelid",
			OldValue: oldGuild.AfkChannelID,
			NewValue: newGuild.AfkChannelID,
		})
	}

	if oldGuild.EmbedChannelID != newGuild.EmbedChannelID {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "guild_embedchannelid",
			OldValue: oldGuild.EmbedChannelID,
			NewValue: newGuild.EmbedChannelID,
		})
	}

	if oldGuild.OwnerID != newGuild.OwnerID {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "guild_ownerid",
			OldValue: oldGuild.OwnerID,
			NewValue: newGuild.OwnerID,
		})
	}

	if oldGuild.Splash != newGuild.Splash {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "guild_splash",
			OldValue: oldGuild.Splash,
			NewValue: newGuild.Splash,
		})
	}

	if oldGuild.AfkTimeout != newGuild.AfkTimeout {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "guild_afktimeout",
			OldValue: strconv.Itoa(oldGuild.AfkTimeout),
			NewValue: strconv.Itoa(newGuild.AfkTimeout),
		})
	}

	if oldGuild.VerificationLevel != newGuild.VerificationLevel {
		var oldVerificationLevel, newVerificationLevel string
		switch oldGuild.VerificationLevel {
		case discordgo.VerificationLevelNone:
			oldVerificationLevel = "none"
			break
		case discordgo.VerificationLevelLow:
			oldVerificationLevel = "low"
			break
		case discordgo.VerificationLevelMedium:
			oldVerificationLevel = "medium"
			break
		case discordgo.VerificationLevelHigh:
			oldVerificationLevel = "high"
			break
		}
		switch newGuild.VerificationLevel {
		case discordgo.VerificationLevelNone:
			newVerificationLevel = "none"
			break
		case discordgo.VerificationLevelLow:
			newVerificationLevel = "low"
			break
		case discordgo.VerificationLevelMedium:
			newVerificationLevel = "medium"
			break
		case discordgo.VerificationLevelHigh:
			newVerificationLevel = "high"
			break
		}
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "guild_verificationlevel",
			OldValue: oldVerificationLevel,
			NewValue: newVerificationLevel,
		})
	}

	if oldGuild.EmbedEnabled != newGuild.EmbedEnabled {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "guild_embedenabled",
			OldValue: StoreBoolAsString(oldGuild.EmbedEnabled),
			NewValue: StoreBoolAsString(newGuild.EmbedEnabled),
		})
	}

	if oldGuild.DefaultMessageNotifications != newGuild.DefaultMessageNotifications {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "guild_defaultmessagenotifications",
			OldValue: strconv.Itoa(oldGuild.DefaultMessageNotifications),
			NewValue: strconv.Itoa(newGuild.DefaultMessageNotifications),
		})
	}

	added, err := EventlogLog(leftAt, guildID, newGuild.ID, models.EventlogTargetTypeGuild, "", models.EventlogTypeGuildUpdate, "", changes, nil, true)
	RelaxLog(err)
	if added {
		err := RequestAuditLogBackfill(guildID, AuditLogBackfillTypeGuildUpdate)
		RelaxLog(err)
	}
}

func OnEventlogChannelUpdate(guildID string, oldChannel, newChannel *discordgo.Channel) {
	leftAt := time.Now()

	changes := make([]models.ElasticEventlogChange, 0)

	if oldChannel.Name != newChannel.Name {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "channel_name",
			OldValue: oldChannel.Name,
			NewValue: newChannel.Name,
		})
	}

	if oldChannel.Topic != newChannel.Topic {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "channel_topic",
			OldValue: oldChannel.Topic,
			NewValue: newChannel.Topic,
		})
	}

	if oldChannel.NSFW != newChannel.NSFW {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "channel_nsfw",
			OldValue: StoreBoolAsString(oldChannel.NSFW),
			NewValue: StoreBoolAsString(newChannel.NSFW),
		})
	}

	if oldChannel.Position != newChannel.Position {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "channel_position",
			OldValue: strconv.Itoa(oldChannel.Position),
			NewValue: strconv.Itoa(newChannel.Position),
		})
	}

	if oldChannel.Bitrate != newChannel.Bitrate {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "channel_bitrate",
			OldValue: strconv.Itoa(oldChannel.Bitrate),
			NewValue: strconv.Itoa(newChannel.Bitrate),
		})
	}

	if oldChannel.ParentID != newChannel.ParentID {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "channel_parentid",
			OldValue: oldChannel.ParentID,
			NewValue: newChannel.ParentID,
		})
	}

	if !ChannelOverwritesMatch(oldChannel.PermissionOverwrites, newChannel.PermissionOverwrites) {
		fmt.Println("permission overwrites changed")
		// TODO: handle permission overwrites
		/*
			changes = append(changes, models.ElasticEventlogChange{
				Key:      "channel_permissionoverwrites",
				OldValue: oldChannel.PermissionOverwrites,
				NewValue: newChannel.PermissionOverwrites,
			})
		*/
	}

	added, err := EventlogLog(leftAt, guildID, newChannel.ID, models.EventlogTargetTypeChannel, "", models.EventlogTypeChannelUpdate, "", changes, nil, true)
	RelaxLog(err)
	if added {
		err := RequestAuditLogBackfill(guildID, AuditLogBackfillTypeChannelUpdate)
		RelaxLog(err)
	}
}

func OnEventlogMemberUpdate(guildID string, oldMember, newMember *discordgo.Member) {
	leftAt := time.Now()

	changes := make([]models.ElasticEventlogChange, 0)

	options := make([]models.ElasticEventlogOption, 0)

	rolesAdded, rolesRemoved := StringSliceDiff(oldMember.Roles, newMember.Roles)

	if len(rolesAdded) > 0 || len(rolesRemoved) > 0 {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "member_roles",
			OldValue: strings.Join(oldMember.Roles, ","),
			NewValue: strings.Join(newMember.Roles, ","),
		})

		if len(rolesAdded) > 0 {
			options = append(options, models.ElasticEventlogOption{
				Key:   "member_roles_added",
				Value: strings.Join(rolesAdded, ","),
			})
		}

		if len(rolesRemoved) > 0 {
			options = append(options, models.ElasticEventlogOption{
				Key:   "member_roles_removed",
				Value: strings.Join(rolesRemoved, ","),
			})
		}
	}

	if oldMember.User.Username+"#"+oldMember.User.Discriminator != newMember.User.Username+"#"+newMember.User.Discriminator {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "member_username",
			OldValue: oldMember.User.Username + "#" + oldMember.User.Discriminator,
			NewValue: newMember.User.Username + "#" + newMember.User.Discriminator,
		})
	}

	if oldMember.Nick != newMember.Nick {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "member_nick",
			OldValue: oldMember.Nick,
			NewValue: newMember.Nick,
		})
	}

	_, err := EventlogLog(leftAt, guildID, newMember.User.ID, models.EventlogTargetTypeUser, "", models.EventlogTypeMemberUpdate, "", changes, options, false)
	RelaxLog(err)
	/*
		backfill? lots of requests because of bot role changes
		if added {
			err := RequestAuditLogBackfill(guildID, AuditLogBackfillTypeChannelUpdate)
			RelaxLog(err)
		}
	*/
}

func OnEventlogRoleUpdate(guildID string, oldRole, newRole *discordgo.Role) {
	leftAt := time.Now()

	changes := make([]models.ElasticEventlogChange, 0)

	if oldRole.Name != newRole.Name {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "role_name",
			OldValue: oldRole.Name,
			NewValue: newRole.Name,
		})
	}

	if oldRole.Managed != newRole.Managed {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "role_managed",
			OldValue: StoreBoolAsString(oldRole.Managed),
			NewValue: StoreBoolAsString(newRole.Managed),
		})
	}

	if oldRole.Mentionable != newRole.Mentionable {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "role_mentionable",
			OldValue: StoreBoolAsString(oldRole.Mentionable),
			NewValue: StoreBoolAsString(newRole.Mentionable),
		})
	}

	if oldRole.Hoist != newRole.Hoist {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "role_hoist",
			OldValue: StoreBoolAsString(oldRole.Hoist),
			NewValue: StoreBoolAsString(newRole.Hoist),
		})
	}

	if oldRole.Color != newRole.Color {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "role_color",
			OldValue: GetHexFromDiscordColor(oldRole.Color),
			NewValue: GetHexFromDiscordColor(newRole.Color),
		})
	}

	if oldRole.Position != newRole.Position {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "role_position",
			OldValue: strconv.Itoa(oldRole.Position),
			NewValue: strconv.Itoa(newRole.Position),
		})
	}

	if oldRole.Permissions != newRole.Permissions {
		changes = append(changes, models.ElasticEventlogChange{
			Key:      "role_permissions",
			OldValue: strconv.Itoa(oldRole.Permissions),
			NewValue: strconv.Itoa(newRole.Permissions),
		})
	}

	added, err := EventlogLog(leftAt, guildID, newRole.ID, models.EventlogTargetTypeRole, "", models.EventlogTypeRoleUpdate, "", changes, nil, true)
	RelaxLog(err)
	if added {
		err := RequestAuditLogBackfill(guildID, AuditLogBackfillTypeRoleUpdate)
		RelaxLog(err)
	}
}

func StoreBoolAsString(input bool) (output string) {
	if input {
		return "yes"
	} else {
		return "no"
	}
}
