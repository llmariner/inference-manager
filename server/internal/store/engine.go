package store

import (
	"errors"

	"gorm.io/gorm"
)

// Engine models the engine.
type Engine struct {
	gorm.Model

	EngineID string `gorm:"uniqueIndex"`

	TenantID string `gorm:"index"`

	// PodName is name of the pods where the engine is connecting to.
	PodName string

	// PodIP is the IP address of the pods where the engine is connecting to.
	PodIP string

	// Status is a marshaled data of the EngineStatus proto message.
	Status []byte

	IsReady bool

	// LastStatusUpdatedAt is the Unix nanoseconds when the status was updated last time.
	LastStatusUpdatedAt int64
}

// CreateOrUpdateEngine creates or updates the engine.
func (s *S) CreateOrUpdateEngine(engine *Engine) error {
	if err := s.db.Where("engine_id = ?", engine.EngineID).First(&Engine{}).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return s.db.Create(&engine).Error
	}
	res := s.db.Model(&Engine{}).
		Where("engine_id = ?", engine.EngineID).
		Updates(map[string]interface{}{
			"tenant_id":              engine.TenantID,
			"pod_name":               engine.PodName,
			"pod_ip":                 engine.PodIP,
			"status":                 engine.Status,
			"is_ready":               engine.IsReady,
			"last_status_updated_at": engine.LastStatusUpdatedAt,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteEngine deletes the engine of the ID.
func (s *S) DeleteEngine(engineID string) error {
	res := s.db.Unscoped().Where("engine_id = ?", engineID).Delete(&Engine{})
	if err := res.Error; err != nil {
		return err
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ListEnginesByTenantID lists engines by tenant ID.
func (s *S) ListEnginesByTenantID(tenantID string) ([]*Engine, error) {
	var engines []*Engine
	if err := s.db.Where("tenant_id = ?", tenantID).Find(&engines).Error; err != nil {
		return nil, err
	}
	return engines, nil
}
