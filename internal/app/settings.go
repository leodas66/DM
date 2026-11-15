package app

import "github.com/vishwa/dm/internal/manager"

// SettingsBackend is the Manager capability used by SettingsService.
type SettingsBackend interface {
	Settings() manager.Settings
	UpdateSettings(manager.Settings) error
}

// SettingsService exposes effective manager settings to delivery layers.
type SettingsService struct {
	backend SettingsBackend
}

// NewSettingsService creates a SettingsService over backend.
func NewSettingsService(backend SettingsBackend) *SettingsService {
	return &SettingsService{backend: backend}
}

// Get returns a copy of the effective settings.
func (s *SettingsService) Get() manager.Settings {
	return s.backend.Settings()
}

// Update persists settings and returns the effective values after defaults.
func (s *SettingsService) Update(settings manager.Settings) (manager.Settings, error) {
	if err := s.backend.UpdateSettings(settings); err != nil {
		return manager.Settings{}, err
	}
	return s.backend.Settings(), nil
}
