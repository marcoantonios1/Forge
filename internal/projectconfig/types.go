package projectconfig

import (
	"fmt"
	"time"
)

type ProjectConfig struct {
	Path     string
	Raw      string
	LoadedAt time.Time
}

func (p *ProjectConfig) SystemPromptBlock() string {
	return fmt.Sprintf("<project_config source=\"forge.md\">\n%s\n</project_config>", p.Raw)
}

func (p *ProjectConfig) IsZero() bool {
	return p == nil || p.Path == ""
}
