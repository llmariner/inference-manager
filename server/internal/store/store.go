package store

import (
	"gorm.io/gorm"
)

// New creates a new store instance.
func New(db *gorm.DB) *S {
	return &S{
		db: db,
	}
}

// S represents the data store.
type S struct {
	db *gorm.DB
}

// AutoMigrate sets up the auto-migration task of the database.
func (s *S) AutoMigrate() error {
	return autoMigrate(s.db)
}

func autoMigrate(db *gorm.DB) error {
	return db.AutoMigrate()
}
