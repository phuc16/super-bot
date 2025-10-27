package commands

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"
)

// respondError sends an error response to an interaction
func respondError(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "❌ " + message,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to send error response")
	}
}

// respondErrorWithID sends an error response with a tracking ID
func respondErrorWithID(s *discordgo.Session, i *discordgo.InteractionCreate, message string, errorID string) {
	fullMessage := fmt.Sprintf("❌ %s\n\n*Error ID: `%s`*", message, errorID)
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: fullMessage,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error().Err(err).Str("error_id", errorID).Msg("Failed to send error response")
	}
}

// respondSuccess sends a success response to an interaction
func respondSuccess(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: message,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to send success response")
	}
}

// followUpSuccess sends a follow-up success message
func followUpSuccess(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	_, err := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: message,
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to send follow-up success message")
	}
}

// followUpError sends a follow-up error message
func followUpError(s *discordgo.Session, i *discordgo.InteractionCreate, message string) {
	_, err := s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
		Content: "❌ " + message,
		Flags:   discordgo.MessageFlagsEphemeral,
	})
	if err != nil {
		log.Error().Err(err).Msg("Failed to send follow-up error message")
	}
}

// followUpErrorWithID sends a follow-up error message with a tracking ID
func followUpErrorWithID(s *discordgo.Session, i *discordgo.InteractionCreate, message string, errorID string) {
	fullMessage := fmt.Sprintf("❌ %s\n\n*Error ID: `%s`*", message, errorID)
	_, err := s.FollowupMessageCreate(i.Interaction, false, &discordgo.WebhookParams{
		Content: fullMessage,
		Flags:   discordgo.MessageFlagsEphemeral,
	})
	if err != nil {
		log.Error().Err(err).Str("error_id", errorID).Msg("Failed to send follow-up error")
	}
}

// hasAdminPermission checks if a member has admin or manage server permission
func hasAdminPermission(member *discordgo.Member) bool {
	if member == nil {
		return false
	}

	permissions := int64(member.Permissions)

	// Check for Administrator or Manage Server permission
	hasAdmin := (permissions & discordgo.PermissionAdministrator) != 0
	hasManageServer := (permissions & discordgo.PermissionManageServer) != 0

	return hasAdmin || hasManageServer
}
