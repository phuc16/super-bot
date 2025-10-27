package migrations

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
)

// Migrator handles database migrations
type Migrator struct {
	db *sql.DB
}

// NewMigrator creates a new migrator instance
func NewMigrator(db *sql.DB) *Migrator {
	return &Migrator{db: db}
}

// Up runs all pending up migrations
func (m *Migrator) Up() error {
	log.Info().Msg("Running database migrations...")

	// Create migrations table if it doesn't exist
	if err := m.createMigrationsTable(); err != nil {
		return err
	}

	// Get current version
	currentVersion, err := m.getCurrentVersion()
	if err != nil {
		return err
	}

	// Find migration files
	migrationsPath := "scripts/migrations"
	files, err := filepath.Glob(filepath.Join(migrationsPath, "*_up.sql"))
	if err != nil {
		return fmt.Errorf("failed to find migration files: %w", err)
	}

	if len(files) == 0 {
		log.Info().Msg("No migrations found")
		return nil
	}

	// Run migrations
	for _, file := range files {
		version := filepath.Base(file)[:3] // Get version from filename (e.g., "001")

		if version <= currentVersion {
			continue // Skip already applied migrations
		}

		log.Info().Str("file", filepath.Base(file)).Msg("Running migration")

		// Read migration file
		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", file, err)
		}

		// Execute migration
		statements := splitSQLStatements(string(content))
		for _, stmt := range statements {
			if stmt == "" {
				continue
			}
			if _, err := m.db.Exec(stmt); err != nil {
				return fmt.Errorf("failed to execute migration %s: %w", file, err)
			}
		}

		// Update version
		if err := m.updateVersion(version); err != nil {
			return fmt.Errorf("failed to update migration version: %w", err)
		}

		log.Info().Str("version", version).Msg("Migration completed")
	}

	log.Info().Str("version", currentVersion).Msg("All migrations completed successfully")
	return nil
}

// Down rolls back the last migration
func (m *Migrator) Down() error {
	log.Info().Msg("Rolling back last migration...")

	// Get current version
	currentVersion, err := m.getCurrentVersion()
	if err != nil {
		return err
	}

	if currentVersion == "000" {
		log.Info().Msg("No migrations to roll back")
		return nil
	}

	// Find down migration file
	downFile := filepath.Join("scripts/migrations", currentVersion+"_initial_schema_down.sql")

	// Read migration file
	content, err := os.ReadFile(downFile)
	if err != nil {
		return fmt.Errorf("failed to read rollback file: %w", err)
	}

	// Execute rollback
	if _, err := m.db.Exec(string(content)); err != nil {
		return fmt.Errorf("failed to execute rollback: %w", err)
	}

	// Update version to previous
	if err := m.updateVersion("000"); err != nil {
		return fmt.Errorf("failed to update version: %w", err)
	}

	log.Info().Msg("Rollback completed")
	return nil
}

// createMigrationsTable creates the migrations tracking table
func (m *Migrator) createMigrationsTable() error {
	query := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(10) PRIMARY KEY,
			applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
	`
	_, err := m.db.Exec(query)
	return err
}

// getCurrentVersion returns the current migration version
func (m *Migrator) getCurrentVersion() (string, error) {
	var version string
	err := m.db.QueryRow("SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1").Scan(&version)
	if err == sql.ErrNoRows {
		return "000", nil
	}
	if err != nil {
		return "", err
	}
	return version, nil
}

// updateVersion records a migration version
func (m *Migrator) updateVersion(version string) error {
	_, err := m.db.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version)
	return err
}

// splitSQLStatements splits SQL content by semicolon, handling multi-line statements
func splitSQLStatements(sql string) []string {
	// Split by semicolon
	parts := strings.Split(sql, ";")

	// Clean up each statement
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		stmt := strings.TrimSpace(part)
		if stmt != "" && !strings.HasPrefix(strings.ToLower(stmt), "delimiter") {
			statements = append(statements, stmt)
		}
	}

	return statements
}
