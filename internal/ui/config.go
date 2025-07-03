package ui

// UIConfig は UI 全体の設定を管理
type UIConfig struct {
	CUI *Config `yaml:"cui"`
}

// GetCUIConfig は CUI 設定を返す（デフォルト値付き）
func (uc *UIConfig) GetCUIConfig() *Config {
	if uc == nil || uc.CUI == nil {
		return defaultConfig()
	}
	
	// デフォルト値をマージ
	cfg := defaultConfig()
	if uc.CUI.Title != "" {
		cfg.Title = uc.CUI.Title
	}
	cfg.Border = uc.CUI.Border
	cfg.EnableColors = uc.CUI.EnableColors
	
	// カラー設定をマージ
	if uc.CUI.Colors.Header != "" {
		cfg.Colors.Header = uc.CUI.Colors.Header
	}
	if uc.CUI.Colors.Footer != "" {
		cfg.Colors.Footer = uc.CUI.Colors.Footer
	}
	if uc.CUI.Colors.Success != "" {
		cfg.Colors.Success = uc.CUI.Colors.Success
	}
	if uc.CUI.Colors.Warning != "" {
		cfg.Colors.Warning = uc.CUI.Colors.Warning
	}
	if uc.CUI.Colors.Error != "" {
		cfg.Colors.Error = uc.CUI.Colors.Error
	}
	if uc.CUI.Colors.ModalBorder != "" {
		cfg.Colors.ModalBorder = uc.CUI.Colors.ModalBorder
	}
	
	return cfg
}
