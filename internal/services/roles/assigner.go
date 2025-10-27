package roles

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/phuc16/VCG-super-bot/internal/models"
)

// Assigner handles Discord role assignment
type Assigner struct {
	db      *gorm.DB
	session *discordgo.Session
}

// NewAssigner creates a new role assigner
func NewAssigner(db *gorm.DB, session *discordgo.Session) *Assigner {
	return &Assigner{
		db:      db,
		session: session,
	}
}

// AssignActiveRole assigns the "Active of the Week" role to top users
func (a *Assigner) AssignActiveRole(ctx context.Context, guildID string, roleName string, topUsers []TopUser) error {
	if len(topUsers) == 0 {
		log.Info().Str("guild_id", guildID).Msg("No users to assign role to")
		return nil
	}

	// Get or create the role
	roleID, err := a.getOrCreateRole(guildID, roleName)
	if err != nil {
		return fmt.Errorf("failed to get or create role: %w", err)
	}

	// Get current role holders
	currentHolders, err := a.getCurrentRoleHolders(guildID, roleID)
	if err != nil {
		return fmt.Errorf("failed to get current role holders: %w", err)
	}

	// Determine who to add and remove
	newHolderIDs := make(map[string]bool)
	for _, user := range topUsers {
		newHolderIDs[user.UserID] = true
	}

	// Remove role from previous holders
	for _, holderID := range currentHolders {
		if !newHolderIDs[holderID] {
			if err := a.removeRole(guildID, holderID, roleID); err != nil {
				log.Error().
					Err(err).
					Str("guild_id", guildID).
					Str("user_id", holderID).
					Str("role_id", roleID).
					Msg("Failed to remove role from previous holder")
				// Continue with others
			} else {
				// Update database
				a.updateUserRoleStatus(ctx, guildID, holderID, false)
			}
		}
	}

	// Assign role to new holders
	successCount := 0
	for _, user := range topUsers {
		if err := a.assignRole(guildID, user.UserID, roleID); err != nil {
			log.Error().
				Err(err).
				Str("guild_id", guildID).
				Str("user_id", user.UserID).
				Str("username", user.Username).
				Str("role_id", roleID).
				Msg("Failed to assign role to user")
			// Continue with others
		} else {
			successCount++
			// Update database
			a.updateUserRoleStatus(ctx, guildID, user.UserID, true)
		}
	}

	log.Info().
		Str("guild_id", guildID).
		Str("role_name", roleName).
		Int("assigned", successCount).
		Int("total", len(topUsers)).
		Msg("Active role assignment complete")

	return nil
}

// getOrCreateRole gets an existing role or creates it if it doesn't exist
func (a *Assigner) getOrCreateRole(guildID string, roleName string) (string, error) {
	// Get existing roles
	roles, err := a.session.GuildRoles(guildID)
	if err != nil {
		return "", fmt.Errorf("failed to fetch guild roles: %w", err)
	}

	// Check if role exists
	for _, role := range roles {
		if role.Name == roleName {
			return role.ID, nil
		}
	}

	// Create role
	log.Info().
		Str("guild_id", guildID).
		Str("role_name", roleName).
		Msg("Creating active role")

	role, err := a.session.GuildRoleCreate(guildID, &discordgo.RoleParams{
		Name:  roleName,
		Color: ptrInt(0x00FF00), // Green color
		Hoist: ptrBool(true),    // Display separately
	})
	if err != nil {
		return "", fmt.Errorf("failed to create role: %w", err)
	}

	return role.ID, nil
}

// getCurrentRoleHolders returns a list of user IDs who currently have the role
func (a *Assigner) getCurrentRoleHolders(guildID string, roleID string) ([]string, error) {
	// Query database for users with active role
	var activities []models.UserActivity

	err := a.db.Where("guild_id = ? AND has_active_role = ?", guildID, true).
		Find(&activities).Error

	if err != nil {
		return nil, err
	}

	holders := make([]string, 0, len(activities))
	for _, activity := range activities {
		holders = append(holders, activity.UserID)
	}

	return holders, nil
}

// assignRole assigns a role to a user
func (a *Assigner) assignRole(guildID string, userID string, roleID string) error {
	err := a.session.GuildMemberRoleAdd(guildID, userID, roleID)
	if err != nil {
		return fmt.Errorf("failed to add role: %w", err)
	}

	log.Debug().
		Str("guild_id", guildID).
		Str("user_id", userID).
		Str("role_id", roleID).
		Msg("Role assigned to user")

	return nil
}

// removeRole removes a role from a user
func (a *Assigner) removeRole(guildID string, userID string, roleID string) error {
	err := a.session.GuildMemberRoleRemove(guildID, userID, roleID)
	if err != nil {
		return fmt.Errorf("failed to remove role: %w", err)
	}

	log.Debug().
		Str("guild_id", guildID).
		Str("user_id", userID).
		Str("role_id", roleID).
		Msg("Role removed from user")

	return nil
}

// updateUserRoleStatus updates the database with role status
func (a *Assigner) updateUserRoleStatus(ctx context.Context, guildID string, userID string, hasRole bool) {
	err := a.db.WithContext(ctx).
		Model(&models.UserActivity{}).
		Where("guild_id = ? AND user_id = ?", guildID, userID).
		Updates(map[string]interface{}{
			"has_active_role": hasRole,
		}).Error

	if err != nil {
		log.Error().
			Err(err).
			Str("guild_id", guildID).
			Str("user_id", userID).
			Bool("has_role", hasRole).
			Msg("Failed to update user role status in database")
	}
}

// ptrInt returns a pointer to an int
func ptrInt(i int) *int {
	return &i
}

// ptrBool returns a pointer to a bool
func ptrBool(b bool) *bool {
	return &b
}
