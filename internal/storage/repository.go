package storage

import (
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Repository provides generic GORM operations
type Repository struct {
	db *gorm.DB
}

// NewRepository creates a new repository instance
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// Create inserts a new record
func (r *Repository) Create(model interface{}) error {
	if err := r.db.Create(model).Error; err != nil {
		return fmt.Errorf("failed to create record: %w", err)
	}
	return nil
}

// FindByID retrieves a record by UUID
func (r *Repository) FindByID(id uuid.UUID, model interface{}) error {
	if err := r.db.First(model, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("record not found: %s", id)
		}
		return fmt.Errorf("failed to find record: %w", err)
	}
	return nil
}

// FindAll retrieves all records
func (r *Repository) FindAll(models interface{}) error {
	if err := r.db.Find(models).Error; err != nil {
		return fmt.Errorf("failed to find records: %w", err)
	}
	return nil
}

// FindWhere retrieves records matching a condition
func (r *Repository) FindWhere(models interface{}, condition string, args ...interface{}) error {
	if err := r.db.Where(condition, args...).Find(models).Error; err != nil {
		return fmt.Errorf("failed to find records: %w", err)
	}
	return nil
}

// Update updates an existing record
func (r *Repository) Update(model interface{}) error {
	if err := r.db.Save(model).Error; err != nil {
		return fmt.Errorf("failed to update record: %w", err)
	}
	return nil
}

// Delete soft deletes a record
func (r *Repository) Delete(model interface{}) error {
	if err := r.db.Delete(model).Error; err != nil {
		return fmt.Errorf("failed to delete record: %w", err)
	}
	return nil
}

// DeletePermanently hard deletes a record
func (r *Repository) DeletePermanently(model interface{}) error {
	if err := r.db.Unscoped().Delete(model).Error; err != nil {
		return fmt.Errorf("failed to permanently delete record: %w", err)
	}
	return nil
}

// Count returns the count of records matching a condition
func (r *Repository) Count(model interface{}, condition string, args ...interface{}) (int64, error) {
	var count int64
	query := r.db.Model(model)

	if condition != "" {
		query = query.Where(condition, args...)
	}

	if err := query.Count(&count).Error; err != nil {
		return 0, fmt.Errorf("failed to count records: %w", err)
	}

	return count, nil
}

// Transaction executes a function within a transaction
func (r *Repository) Transaction(fn func(*gorm.DB) error) error {
	return r.db.Transaction(fn)
}

// GetDB returns the underlying database instance
func (r *Repository) GetDB() *gorm.DB {
	return r.db
}
