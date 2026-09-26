package database

import (
	"fmt"
	"time"

	"mobile-backend-go/constants"
	"mobile-backend-go/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type workspaceMemberLookupRow struct {
	MemberID           uint
	MemberCreatedAt    time.Time
	MemberUpdatedAt    time.Time
	WorkspaceID        uint
	UserID             uint
	Role               string
	WorkspaceCreatedAt time.Time
	WorkspaceUpdatedAt time.Time
	WorkspaceName      string
	WorkspaceSlug      string
	WorkspaceCurrency  string
	AccountID          *uint
	PersonalUserID     *uint
}

// FindPersonalWorkspaceMember returns the user's active personal workspace membership.
func FindPersonalWorkspaceMember(db *gorm.DB, userID uint) (models.WorkspaceMember, bool, error) {
	return findWorkspaceMember(db, userID, 0, true)
}

// FindWorkspaceMember returns an active membership in an active workspace.
func FindWorkspaceMember(db *gorm.DB, userID uint, workspaceID uint) (models.WorkspaceMember, bool, error) {
	return findWorkspaceMember(db, userID, workspaceID, false)
}

// EnsurePersonalWorkspaceForUser returns the user's personal workspace membership,
// creating the workspace and owner membership when they do not exist yet.
func EnsurePersonalWorkspaceForUser(db *gorm.DB, userID uint) (models.WorkspaceMember, error) {
	var member models.WorkspaceMember

	err := db.Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.Select("id").First(&user, userID).Error; err != nil {
			return err
		}

		var found bool
		var err error
		member, found, err = FindPersonalWorkspaceMember(tx, userID)
		if err != nil {
			return err
		}
		if found {
			return nil
		}

		workspace := models.Workspace{
			Name:           "Personal workspace",
			Slug:           fmt.Sprintf("personal-%d", userID),
			PersonalUserID: &userID,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&workspace).Error; err != nil {
			return err
		}

		result := tx.Where("personal_user_id = ?", userID).Limit(1).Find(&workspace)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("failed to create personal workspace for user %d", userID)
		}

		newMember := models.WorkspaceMember{
			WorkspaceID: workspace.ID,
			UserID:      userID,
			Role:        constants.WorkspaceRoleOwner,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&newMember).Error; err != nil {
			return err
		}

		var foundAfterCreate bool
		member, foundAfterCreate, err = FindPersonalWorkspaceMember(tx, userID)
		if err != nil {
			return err
		}
		if !foundAfterCreate {
			return fmt.Errorf("failed to create personal workspace membership for user %d", userID)
		}
		return nil
	})

	return member, err
}

// BackfillPersonalWorkspaces creates a default personal workspace for every user.
func BackfillPersonalWorkspaces(db *gorm.DB) error {
	var users []models.User
	if err := db.Find(&users).Error; err != nil {
		return err
	}

	for _, user := range users {
		if _, err := EnsurePersonalWorkspaceForUser(db, user.ID); err != nil {
			return fmt.Errorf("backfill personal workspace for user %d: %w", user.ID, err)
		}
	}

	return nil
}

func findWorkspaceMember(db *gorm.DB, userID uint, workspaceID uint, personal bool) (models.WorkspaceMember, bool, error) {
	var row workspaceMemberLookupRow
	query := `
SELECT
  wm.id AS member_id,
  wm.created_at AS member_created_at,
  wm.updated_at AS member_updated_at,
  wm.workspace_id AS workspace_id,
  wm.user_id AS user_id,
  wm.role AS role,
  w.created_at AS workspace_created_at,
  w.updated_at AS workspace_updated_at,
  w.name AS workspace_name,
  w.slug AS workspace_slug,
  w.currency AS workspace_currency,
  w.account_id AS account_id,
  w.personal_user_id AS personal_user_id
FROM workspace_members wm
JOIN workspaces w ON w.id = wm.workspace_id AND w.deleted_at IS NULL
WHERE wm.user_id = ? AND wm.deleted_at IS NULL`
	args := []interface{}{userID}
	if personal {
		query += ` AND w.personal_user_id = ?`
		args = append(args, userID)
	} else {
		query += ` AND wm.workspace_id = ?`
		args = append(args, workspaceID)
	}
	query += ` LIMIT 1`

	result := db.Raw(query, args...).Scan(&row)
	if result.Error != nil {
		return models.WorkspaceMember{}, false, result.Error
	}
	if result.RowsAffected == 0 {
		return models.WorkspaceMember{}, false, nil
	}

	member := models.WorkspaceMember{
		ID:          row.MemberID,
		CreatedAt:   row.MemberCreatedAt,
		UpdatedAt:   row.MemberUpdatedAt,
		WorkspaceID: row.WorkspaceID,
		UserID:      row.UserID,
		Role:        row.Role,
		Workspace: models.Workspace{
			ID:             row.WorkspaceID,
			CreatedAt:      row.WorkspaceCreatedAt,
			UpdatedAt:      row.WorkspaceUpdatedAt,
			Name:           row.WorkspaceName,
			Slug:           row.WorkspaceSlug,
			Currency:       row.WorkspaceCurrency,
			AccountID:      row.AccountID,
			PersonalUserID: row.PersonalUserID,
		},
	}

	return member, true, nil
}
