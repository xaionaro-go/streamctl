package config

type DashboardConfig struct {
	Elements map[string]DashboardElementConfig `yaml:"elements"` // TODO: rename this to `video_elements`
}
